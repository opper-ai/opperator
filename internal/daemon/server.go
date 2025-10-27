package daemon

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"opperator/internal/agent"
	"opperator/internal/config"
	"opperator/internal/credentials"
	"opperator/internal/ipc"
	"opperator/internal/protocol"
	"opperator/internal/taskqueue"
	"opperator/pkg/migration"
	"tui/components/sidebar"

	_ "modernc.org/sqlite"
)

type Server struct {
	manager     *agent.Manager
	tasks       *taskqueue.Manager
	listener    net.Listener
	lock        *processLock
	db          *sql.DB
	stateBroker *Broker[AgentStateChange]
	taskBroker  *Broker[TaskEvent]
	logFile     *os.File
}

func NewServer() (*Server, error) {
	lock, err := acquireProcessLock()
	if err != nil {
		return nil, err
	}

	logPath, err := config.GetDaemonLogPath()
	if err != nil {
		lock.Release()
		return nil, fmt.Errorf("failed to get daemon log path: %w", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		lock.Release()
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)

	log.Printf("=== Daemon starting ===")
	log.Printf("Log file: %s", logPath)

	// Ensure config exists and get path
	if err := config.EnsureConfigExists(); err != nil {
		logFile.Close()
		lock.Release()
		return nil, fmt.Errorf("failed to initialize config: %w", err)
	}

	configPath, err := config.GetConfigFile()
	if err != nil {
		logFile.Close()
		lock.Release()
		return nil, err
	}

	log.Printf("Loading agent config from: %s", configPath)
	manager, err := agent.New(configPath)
	if err != nil {
		logFile.Close()
		lock.Release()
		return nil, err
	}

	dbPath, err := config.GetDatabasePath()
	if err != nil {
		logFile.Close()
		lock.Release()
		return nil, err
	}
	log.Printf("Opening database: %s", dbPath)
	db, err := sql.Open("sqlite", dbPath+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		logFile.Close()
		lock.Release()
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	migrationRunner := migration.NewRunner(db)
	if err := migrationRunner.Run(); err != nil {
		db.Close()
		logFile.Close()
		lock.Release()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	taskRunner := newDaemonToolRunner()
	agentRunner := newDaemonAgentRunner(manager)
	taskManager, err := taskqueue.NewManager(context.Background(), db, taskRunner, agentRunner)
	if err != nil {
		db.Close()
		logFile.Close()
		lock.Release()
		return nil, err
	}

	stateBroker := NewBroker[AgentStateChange]()
	taskBroker := NewBroker[TaskEvent]()

	taskManager.SetEventSink(func(ev taskqueue.TaskEvent) {
		var taskCopy *taskqueue.Task
		if ev.Task != nil {
			taskCopy = ev.Task.Clone()
		}
		taskBroker.Publish(TaskEvent{
			Type: TaskEventType(ev.Type),
			Task: taskCopy,
		})
	})

	server := &Server{
		manager:     manager,
		tasks:       taskManager,
		lock:        lock,
		db:          db,
		stateBroker: stateBroker,
		taskBroker:  taskBroker,
		logFile:     logFile,
	}

	manager.SetStateChangeCallback(func(agentName string, changeType string, data interface{}) {
		server.publishStateChange(agentName, changeType, data)
	})

	// Start previously running agents
	server.startPreviouslyRunningAgents()

	return server, nil
}

func (s *Server) Start() (err error) {
	socketPath, err := config.GetSocketPath()
	if err != nil {
		return err
	}

	_ = os.Remove(socketPath)

	defer func() {
		if err != nil {
			s.releaseLock()
		}
	}()

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	s.listener = l
	if err := os.Chmod(socketPath, 0660); err != nil {
		log.Printf("daemon: failed to update socket permissions: %v", err)
	}

	log.Printf("Daemon started, listening on %s", socketPath)

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				log.Printf("temporary accept error: %v", err)
				continue
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("daemon accept: %w", err)
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	connID := fmt.Sprintf("%p", conn)
	log.Printf("[Connection %s] New connection from %s", connID, conn.RemoteAddr())

	reader := bufio.NewReader(conn)
	requestCount := 0
	for {
		data, err := reader.ReadBytes('\n')
		if err != nil {
			log.Printf("[Connection %s] Connection closed after %d requests", connID, requestCount)
			return
		}

		req, err := ipc.DecodeRequest(data)
		if err != nil {
			log.Printf("[Connection %s] Invalid request: %v", connID, err)
			resp := ipc.Response{Success: false, Error: "invalid request"}
			b, _ := ipc.EncodeResponse(resp)
			_, _ = conn.Write(append(b, '\n'))
			continue
		}

		requestCount++
		log.Printf("[Connection %s] Request #%d: type=%s, agent=%s", connID, requestCount, req.Type, req.AgentName)

		if req.Type == ipc.RequestWatchToolTask {
			log.Printf("[Connection %s] Switching to tool task streaming mode", connID)
			s.streamToolTask(conn, req)
			return
		}

		if req.Type == ipc.RequestWatchAgentState {
			log.Printf("[Connection %s] Switching to agent state streaming mode", connID)
			s.streamAgentState(conn, req)
			return
		}

		if req.Type == ipc.RequestWatchAllTasks {
			log.Printf("[Connection %s] Switching to task streaming mode", connID)
			s.streamAllTasks(conn, req)
			return
		}

		resp := s.processRequest(req)
		b, _ := ipc.EncodeResponse(resp)
		_, _ = conn.Write(append(b, '\n'))
		log.Printf("[Connection %s] Request #%d completed: success=%v", connID, requestCount, resp.Success)
	}
}

func (s *Server) streamAgentState(conn net.Conn, req ipc.Request) {
	log.Printf("[AgentStateStream] New client connected to agent state stream")
	if s.stateBroker == nil {
		log.Printf("[AgentStateStream] ERROR: state broker unavailable")
		resp := ipc.Response{Success: false, Error: "state broker unavailable"}
		if b, err := ipc.EncodeResponse(resp); err == nil {
			_, _ = conn.Write(append(b, '\n'))
		}
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe to agent state changes
	events := s.stateBroker.Subscribe(ctx)
	log.Printf("[AgentStateStream] Client subscribed to state changes")

	if b, err := ipc.EncodeResponse(ipc.Response{Success: true}); err == nil {
		if _, writeErr := conn.Write(append(b, '\n')); writeErr != nil {
			log.Printf("[AgentStateStream] Failed to write success response: %v", writeErr)
			return
		}
	}

	// Stream events
	encoder := json.NewEncoder(conn)
	eventCount := 0
	for ev := range events {
		eventCount++
		log.Printf("[AgentStateStream] Streaming event #%d: type=%s, agent=%s", eventCount, ev.Type, ev.AgentName)
		if ev.Type == AgentStateSections && len(ev.CustomSections) > 0 {
			log.Printf("[AgentStateStream] Event contains %d custom sections", len(ev.CustomSections))
		}
		payload := convertAgentStateEvent(ev)
		if err := encoder.Encode(payload); err != nil {
			log.Printf("[AgentStateStream] Failed to encode/send event: %v", err)
			return
		}
	}
	log.Printf("[AgentStateStream] Client disconnected after receiving %d events", eventCount)
}

func (s *Server) streamAllTasks(conn net.Conn, req ipc.Request) {
	log.Printf("[TaskStream] New client connected to task stream")
	if s.taskBroker == nil {
		log.Printf("[TaskStream] ERROR: task broker unavailable")
		resp := ipc.Response{Success: false, Error: "task broker unavailable"}
		if b, err := ipc.EncodeResponse(resp); err == nil {
			_, _ = conn.Write(append(b, '\n'))
		}
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe to task events
	events := s.taskBroker.Subscribe(ctx)
	log.Printf("[TaskStream] Client subscribed to task events")

	if b, err := ipc.EncodeResponse(ipc.Response{Success: true}); err == nil {
		if _, writeErr := conn.Write(append(b, '\n')); writeErr != nil {
			log.Printf("[TaskStream] Failed to write success response: %v", writeErr)
			return
		}
	}

	encoder := json.NewEncoder(conn)

	// Emit snapshot of currently active tasks.
	if s.tasks != nil {
		initial := s.tasks.ActiveTasks()
		for _, task := range initial {
			if task == nil {
				continue
			}
			payload := ipc.ToolTaskEvent{
				Type: string(taskqueue.TaskEventSnapshot),
				Task: convertTask(task),
			}
			if payload.Task == nil {
				continue
			}
			if err := encoder.Encode(payload); err != nil {
				log.Printf("[TaskStream] Failed to send initial task snapshot: %v", err)
				return
			}
		}
	}

	// Stream events
	eventCount := 0
	for ev := range events {
		eventCount++
		taskID := ""
		if ev.Task != nil {
			taskID = strings.TrimSpace(ev.Task.ID)
		}
		log.Printf("[TaskStream] Streaming event #%d: type=%s, taskID=%s", eventCount, ev.Type, taskID)

		// Convert to ipc.ToolTaskEvent
		payload := ipc.ToolTaskEvent{
			Type: string(ev.Type),
			Task: convertTask(ev.Task),
		}

		if payload.Task == nil {
			continue
		}

		if err := encoder.Encode(payload); err != nil {
			log.Printf("[TaskStream] Failed to encode/send event: %v", err)
			return
		}
	}
	log.Printf("[TaskStream] Client disconnected after receiving %d events", eventCount)
}

func (s *Server) streamToolTask(conn net.Conn, req ipc.Request) {
	if s.tasks == nil {
		resp := ipc.Response{Success: false, Error: "tool task manager unavailable"}
		if b, err := ipc.EncodeResponse(resp); err == nil {
			_, _ = conn.Write(append(b, '\n'))
		}
		return
	}
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		resp := ipc.Response{Success: false, Error: "task id is required"}
		if b, err := ipc.EncodeResponse(resp); err == nil {
			_, _ = conn.Write(append(b, '\n'))
		}
		return
	}
	events, cancel, err := s.tasks.SubscribeTask(taskID)
	if err != nil {
		resp := ipc.Response{Success: false, Error: err.Error()}
		if b, encodeErr := ipc.EncodeResponse(resp); encodeErr == nil {
			_, _ = conn.Write(append(b, '\n'))
		}
		return
	}
	defer cancel()
	if b, err := ipc.EncodeResponse(ipc.Response{Success: true}); err == nil {
		if _, writeErr := conn.Write(append(b, '\n')); writeErr != nil {
			return
		}
	}
	encoder := json.NewEncoder(conn)
	for ev := range events {
		payload := convertTaskEvent(ev)
		if err := encoder.Encode(payload); err != nil {
			return
		}
	}
}

func (s *Server) shutdown() ipc.Response {
	// Schedule daemon shutdown
	go func() {
		time.Sleep(100 * time.Millisecond) // Give time to send response
		s.Stop()
		os.Exit(0)
	}()

	return ipc.Response{Success: true}
}

// processRequest routes requests to the appropriate handlers.
func (s *Server) processRequest(req ipc.Request) ipc.Response {
	switch req.Type {
	case ipc.RequestListAgents:
		return s.listAgents()
	case ipc.RequestStartAgent:
		if err := s.manager.StartAgent(req.AgentName); err != nil {
			return ipc.Response{Success: false, Error: err.Error()}
		}
		return ipc.Response{Success: true}
	case ipc.RequestStopAgent:
		if err := s.manager.StopAgent(req.AgentName); err != nil {
			return ipc.Response{Success: false, Error: err.Error()}
		}
		return ipc.Response{Success: true}
	case ipc.RequestRestartAgent:
		if err := s.manager.RestartAgent(req.AgentName); err != nil {
			return ipc.Response{Success: false, Error: err.Error()}
		}
		return ipc.Response{Success: true}
	case ipc.RequestStopAll:
		if err := s.manager.StopAll(); err != nil {
			return ipc.Response{Success: false, Error: err.Error()}
		}
		return ipc.Response{Success: true}
	case ipc.RequestGetLogs:
		ag, err := s.manager.GetAgent(req.AgentName)
		if err != nil {
			return ipc.Response{Success: false, Error: err.Error()}
		}
		return ipc.Response{Success: true, Logs: ag.GetLogs()}
	case ipc.RequestGetCustomSections:
		log.Printf("[CustomSections] Request to get custom sections for agent: %s", req.AgentName)
		ag, err := s.manager.GetAgent(req.AgentName)
		if err != nil {
			log.Printf("[CustomSections] Failed to get agent %s: %v", req.AgentName, err)
			return ipc.Response{Success: false, Error: err.Error()}
		}
		sections := ag.CustomSections()
		log.Printf("[CustomSections] Retrieved %d custom sections for agent %s", len(sections), req.AgentName)
		for i, sec := range sections {
			log.Printf("[CustomSections]   Section %d: ID=%s, Title=%s, Collapsed=%v, ContentLength=%d",
				i, sec.ID, sec.Title, sec.Collapsed, len(sec.Content))
		}
		return ipc.Response{Success: true, Sections: sections}
	case ipc.RequestCommand:
		if req.Command == "" {
			return ipc.Response{Success: false, Error: "command is required"}
		}
		resp, err := s.manager.InvokeCommand(req.AgentName, req.Command, req.Args, req.WorkingDir, 10*time.Second)
		if err != nil {
			return ipc.Response{Success: false, Error: err.Error()}
		}
		cmdResp := &ipc.CommandResponse{
			Success: resp.Success,
			Error:   resp.Error,
			Result:  resp.Result,
		}
		return ipc.Response{Success: true, Command: cmdResp}
	case ipc.RequestListCommands:
		commands, err := s.manager.ListCommands(req.AgentName, 0)
		if err != nil {
			return ipc.Response{Success: false, Error: err.Error()}
		}
		return ipc.Response{Success: true, Commands: commands}
	case ipc.RequestReloadConfig:
		if err := s.manager.ReloadConfigManual(); err != nil {
			return ipc.Response{Success: false, Error: err.Error()}
		}
		return ipc.Response{Success: true}
	case ipc.RequestShutdown:
		return s.shutdown()
	case ipc.RequestLifecycleEvent:
		ag, err := s.manager.GetAgent(req.AgentName)
		if err != nil {
			return ipc.Response{Success: true}
		}
		if err := ag.SendLifecycleEvent(req.LifecycleType, req.LifecycleData); err != nil {
			return ipc.Response{Success: false, Error: err.Error()}
		}
		return ipc.Response{Success: true}
	case ipc.RequestSubmitToolTask:
		if s.tasks == nil {
			return ipc.Response{Success: false, Error: "tool task manager unavailable"}
		}
		task, err := s.tasks.Submit(context.Background(), taskqueue.SubmitRequest{
			ToolName:    req.ToolName,
			Args:        req.ToolArgs,
			WorkingDir:  req.WorkingDir,
			SessionID:   req.SessionID,
			CallID:      req.CallID,
			Mode:        req.Mode,
			AgentName:   req.AgentName,
			Command:     req.Command,
			CommandArgs: req.CommandArgs,
			Origin:      req.Origin,
			ClientID:    req.ClientID,
		})
		if err != nil {
			return ipc.Response{Success: false, Error: err.Error()}
		}
		return ipc.Response{Success: true, Task: convertTask(task)}
	case ipc.RequestGetToolTask:
		if s.tasks == nil {
			return ipc.Response{Success: false, Error: "tool task manager unavailable"}
		}
		task, ok := s.tasks.Get(req.TaskID)
		if !ok {
			return ipc.Response{Success: false, Error: "task not found"}
		}
		return ipc.Response{Success: true, Task: convertTask(task)}
	case ipc.RequestListToolTasks:
		if s.tasks == nil {
			return ipc.Response{Success: false, Error: "tool task manager unavailable"}
		}
		tasks := s.tasks.List()
		converted := make([]*ipc.ToolTask, 0, len(tasks))
		for _, task := range tasks {
			converted = append(converted, convertTask(task))
		}
		return ipc.Response{Success: true, Tasks: converted}
	case ipc.RequestDeleteToolTask:
		if s.tasks == nil {
			return ipc.Response{Success: false, Error: "tool task manager unavailable"}
		}
		taskID := strings.TrimSpace(req.TaskID)
		callID := strings.TrimSpace(req.CallID)
		sessionID := strings.TrimSpace(req.SessionID)
		switch {
		case taskID != "":
			if _, err := s.tasks.DeleteTask(context.Background(), taskID); err != nil {
				return ipc.Response{Success: false, Error: err.Error()}
			}
		case callID != "":
			if _, err := s.tasks.DeleteTasksByCall(context.Background(), callID); err != nil {
				return ipc.Response{Success: false, Error: err.Error()}
			}
		case sessionID != "":
			if _, err := s.tasks.DeleteTasksBySession(context.Background(), sessionID); err != nil {
				return ipc.Response{Success: false, Error: err.Error()}
			}
		default:
			return ipc.Response{Success: false, Error: "missing task identifier"}
		}
		return ipc.Response{Success: true}
	case ipc.RequestToolTaskMetrics:
		if s.tasks == nil {
			return ipc.Response{Success: false, Error: "tool task manager unavailable"}
		}
		metrics := s.tasks.MetricsSnapshot()
		return ipc.Response{Success: true, Metrics: convertTaskMetrics(metrics)}
	case ipc.RequestGetSecret:
		return s.getSecret(req.SecretName)
	case ipc.RequestSetSecret:
		return s.setSecret(req.SecretName, req.SecretValue, req.Mode)
	case ipc.RequestDeleteSecret:
		return s.deleteSecret(req.SecretName)
	case ipc.RequestListSecrets:
		return s.listSecrets()
	case ipc.RequestGetAgentConfig:
		ag, err := s.manager.GetAgent(req.AgentName)
		if err != nil {
			return ipc.Response{Success: false, Error: err.Error()}
		}
		return ipc.Response{Success: true, ProcessRoot: ag.Config.ProcessRoot}
	default:
		return ipc.Response{Success: false, Error: "unknown request type"}
	}
}

func (s *Server) getSecret(name string) ipc.Response {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ipc.Response{Success: false, Error: "secret name is required"}
	}
	value, err := credentials.GetSecret(trimmed)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return ipc.Response{Success: false, Error: fmt.Sprintf("secret %q not found", trimmed)}
		}
		return ipc.Response{Success: false, Error: err.Error()}
	}
	if err := credentials.RegisterSecret(trimmed); err != nil {
		return ipc.Response{Success: false, Error: err.Error()}
	}
	return ipc.Response{Success: true, Secret: value}
}

func (s *Server) setSecret(name, value, mode string) ipc.Response {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return ipc.Response{Success: false, Error: "secret name is required"}
	}

	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return ipc.Response{Success: false, Error: "secret value is required"}
	}

	exists, err := credentials.HasSecret(trimmedName)
	if err != nil {
		return ipc.Response{Success: false, Error: err.Error()}
	}

	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "upsert", "update", "create":
		// Allow create/update via mode but preserve CLI semantics when possible.
		if strings.EqualFold(strings.TrimSpace(mode), "create") && exists {
			return ipc.Response{Success: false, Error: fmt.Sprintf("secret %q already exists", trimmedName)}
		}
		if strings.EqualFold(strings.TrimSpace(mode), "update") && !exists {
			return ipc.Response{Success: false, Error: fmt.Sprintf("secret %q is not stored", trimmedName)}
		}
	default:
		return ipc.Response{Success: false, Error: fmt.Sprintf("unsupported secret mode %q", mode)}
	}

	if err := credentials.SetSecret(trimmedName, trimmedValue); err != nil {
		return ipc.Response{Success: false, Error: err.Error()}
	}
	if err := credentials.RegisterSecret(trimmedName); err != nil {
		return ipc.Response{Success: false, Error: err.Error()}
	}

	return ipc.Response{Success: true}
}

func (s *Server) deleteSecret(name string) ipc.Response {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return ipc.Response{Success: false, Error: "secret name is required"}
	}
	if err := credentials.DeleteSecret(trimmedName); err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return ipc.Response{Success: false, Error: fmt.Sprintf("secret %q not found", trimmedName)}
		}
		return ipc.Response{Success: false, Error: err.Error()}
	}
	if err := credentials.UnregisterSecret(trimmedName); err != nil {
		return ipc.Response{Success: false, Error: err.Error()}
	}
	return ipc.Response{Success: true}
}

func (s *Server) listSecrets() ipc.Response {
	names, err := credentials.ListSecrets()
	if err != nil {
		return ipc.Response{Success: false, Error: err.Error()}
	}
	return ipc.Response{Success: true, Secrets: names}
}

// listAgents assembles process info for all agents.
func (s *Server) listAgents() ipc.Response {
	agents := s.manager.GetAllAgents()
	infos := make([]*ipc.ProcessInfo, len(agents))

	for i, a := range agents {
		uptime := int64(0)
		if a.GetStatus() == agent.StatusRunning {
			uptime = int64(time.Since(a.StartTime).Seconds())
		}

		infos[i] = &ipc.ProcessInfo{
			Name:         a.Config.Name,
			Description:  a.Description(),
			Status:       a.GetStatus(),
			PID:          a.PID,
			RestartCount: a.RestartCount,
			Uptime:       uptime,
			SystemPrompt: a.SystemPrompt(),
			Color:        a.Color(),
		}
	}

	return ipc.Response{Success: true, Processes: infos}
}

//

func (s *Server) Stop() {
	log.Printf("=== Daemon stopping ===")
	// Snapshot running agents to support auto-restart on next start
	s.manager.SnapshotRunningAgents()
	// Stop agents while preserving state
	s.manager.StopAllPreservingState()
	// Cleanup scheduler, watchers, etc.
	s.manager.Cleanup()
	if s.tasks != nil {
		s.tasks.Shutdown()
	}
	if s.stateBroker != nil {
		s.stateBroker.Shutdown()
	}
	if s.taskBroker != nil {
		s.taskBroker.Shutdown()
	}
	if s.db != nil {
		_ = s.db.Close()
		s.db = nil
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	if socketPath, err := config.GetSocketPath(); err == nil {
		_ = os.Remove(socketPath)
	}
	s.releaseLock()
	if s.logFile != nil {
		log.Printf("Daemon stopped")
		_ = s.logFile.Close()
		s.logFile = nil
	}
}

func (s *Server) releaseLock() {
	if s.lock == nil {
		return
	}
	if err := s.lock.Release(); err != nil {
		log.Printf("failed to release daemon lock: %v", err)
	}
	s.lock = nil
}

func (s *Server) publishStateChange(agentName string, changeType string, data interface{}) {
	if s.stateBroker == nil {
		return
	}

	log.Printf("[StateChange] Publishing state change for agent %s: type=%s", agentName, changeType)

	var change AgentStateChange
	change.AgentName = agentName

	switch changeType {
	case "sections":
		change.Type = AgentStateSections
		if sections, ok := data.([]sidebar.CustomSection); ok {
			change.CustomSections = sections
			log.Printf("[StateChange] Publishing %d custom sections for agent %s", len(sections), agentName)
			for i, sec := range sections {
				log.Printf("[StateChange]   Section %d: ID=%s, Title=%s, Collapsed=%v, ContentLength=%d",
					i, sec.ID, sec.Title, sec.Collapsed, len(sec.Content))
			}
		} else {
			log.Printf("[StateChange] WARNING: sections data is not []sidebar.CustomSection, got type %T", data)
		}
	case "metadata":
		change.Type = AgentStateMetadata
		switch meta := data.(type) {
		case agent.MetadataUpdate:
			change.Description = meta.Description
			change.SystemPrompt = meta.SystemPrompt
			change.Color = meta.Color
			log.Printf("[StateChange] Publishing metadata change for agent %s (description len=%d, prompt len=%d, color=%s)",
				agentName, len(meta.Description), len(meta.SystemPrompt), meta.Color)
		case *agent.MetadataUpdate:
			if meta != nil {
				change.Description = meta.Description
				change.SystemPrompt = meta.SystemPrompt
				change.Color = meta.Color
				log.Printf("[StateChange] Publishing metadata change for agent %s (description len=%d, prompt len=%d, color=%s)",
					agentName, len(meta.Description), len(meta.SystemPrompt), meta.Color)
			}
		default:
			log.Printf("[StateChange] WARNING: metadata data is not agent.MetadataUpdate, got type %T", data)
		}
	case "logs":
		change.Type = AgentStateLogs
		if logs, ok := data.([]string); ok {
			change.Logs = logs
			log.Printf("[StateChange] Publishing %d log entries for agent %s", len(logs), agentName)
		}
	case "log_entry":
		change.Type = AgentStateLogs
		if logEntry, ok := data.(string); ok {
			change.LogEntry = logEntry
			log.Printf("[StateChange] Publishing single log entry for agent %s: %s", agentName, logEntry)
		}
	case "commands":
		change.Type = AgentStateCommands
		if commands, ok := data.([]protocol.CommandDescriptor); ok {
			change.Commands = commands
			log.Printf("[StateChange] Publishing %d command definition(s) for agent %s", len(commands), agentName)
		} else {
			log.Printf("[StateChange] WARNING: commands data is not []protocol.CommandDescriptor, got type %T", data)
		}
	case "status":
		change.Type = AgentStateStatus
		if status, ok := data.(string); ok {
			change.Status = status
			log.Printf("[StateChange] Publishing status change for agent %s: %s", agentName, status)
		}
	default:
		log.Printf("[StateChange] WARNING: Unknown change type %s for agent %s", changeType, agentName)
		return // Unknown change type
	}

	s.stateBroker.Publish(change)
	log.Printf("[StateChange] State change published successfully")
}

//

func convertAgentStateEvent(ev AgentStateChange) ipc.AgentStateEvent {
	return ipc.AgentStateEvent{
		Type:           string(ev.Type),
		AgentName:      ev.AgentName,
		Description:    ev.Description,
		SystemPrompt:   ev.SystemPrompt,
		Color:          ev.Color,
		Logs:           ev.Logs,
		LogEntry:       ev.LogEntry,
		CustomSections: ev.CustomSections,
		Status:         ev.Status,
		Commands:       ev.Commands,
	}
}

func convertTaskEvent(ev taskqueue.TaskEvent) ipc.ToolTaskEvent {
	result := ipc.ToolTaskEvent{Type: string(ev.Type), Error: ev.Error}
	if ev.Task != nil {
		result.Task = convertTask(ev.Task)
	}
	if ev.Progress != nil {
		entry := *ev.Progress
		result.Progress = &ipc.ToolTaskProgress{
			Timestamp: entry.Timestamp.Format(time.RFC3339Nano),
			Text:      entry.Text,
			Metadata:  entry.Metadata,
			Status:    entry.Status,
		}
	}
	return result
}

func convertTaskMetrics(snapshot taskqueue.MetricsSnapshot) *ipc.ToolTaskMetrics {
	return &ipc.ToolTaskMetrics{
		Submitted:   snapshot.Submitted,
		InFlight:    snapshot.InFlight,
		Completed:   snapshot.Completed,
		Failed:      snapshot.Failed,
		QueueDepth:  snapshot.QueueDepth,
		WorkerCount: snapshot.WorkerCount,
	}
}

func convertTask(task *taskqueue.Task) *ipc.ToolTask {
	if task == nil {
		return nil
	}
	converted := &ipc.ToolTask{
		ID:          task.ID,
		ToolName:    task.ToolName,
		Args:        task.Args,
		WorkingDir:  task.WorkingDir,
		SessionID:   task.SessionID,
		CallID:      task.CallID,
		Mode:        task.Mode,
		AgentName:   task.AgentName,
		CommandName: task.CommandName,
		CommandArgs: task.CommandArgs,
		Origin:      task.Origin,
		ClientID:    task.ClientID,
		Status:      string(task.Status),
		Result:      task.Result,
		Metadata:    task.Metadata,
		Error:       task.Error,
		CreatedAt:   task.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:   task.UpdatedAt.Format(time.RFC3339Nano),
	}
	if task.CompletedAt != nil {
		converted.CompletedAt = task.CompletedAt.Format(time.RFC3339Nano)
	}
	if len(task.Progress) > 0 {
		converted.Progress = make([]ipc.ToolTaskProgress, 0, len(task.Progress))
		for _, entry := range task.Progress {
			converted.Progress = append(converted.Progress, ipc.ToolTaskProgress{
				Timestamp: entry.Timestamp.Format(time.RFC3339Nano),
				Text:      entry.Text,
				Metadata:  entry.Metadata,
				Status:    entry.Status,
			})
		}
	}
	return converted
}

//

// startPreviouslyRunningAgents restarts agents that were running when daemon last stopped
func (s *Server) startPreviouslyRunningAgents() {
	allAgents := s.manager.GetAllAgents()

	agentConfigs := make(map[string]bool)
	for _, agent := range allAgents {
		agentConfigs[agent.Config.Name] = agent.Config.StartWithDaemonEnabled()
	}

	previouslyRunning := s.manager.GetPreviouslyRunningAgents()

	for _, agentName := range previouslyRunning {
		if autoStart, exists := agentConfigs[agentName]; !exists || !autoStart {
			continue
		}

		// Start the agent
		if err := s.manager.StartAgent(agentName); err != nil {
			// Log the error but continue with other agents
			log.Printf("Failed to auto-start agent %s: %v", agentName, err)
		} else {
			log.Printf("Auto-started agent: %s", agentName)
		}
	}
}
