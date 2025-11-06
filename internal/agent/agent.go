package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"opperator/config"
	"opperator/internal/protocol"
	"tui/components/sidebar"
)

type ProcessStatus string

const (
	StatusStopped  ProcessStatus = "stopped"
	StatusRunning  ProcessStatus = "running"
	StatusCrashed  ProcessStatus = "crashed"
	StatusStopping ProcessStatus = "stopping"
)

type Agent struct {
	Config         AgentConfig
	Status         ProcessStatus
	PID            int
	StartTime      time.Time
	RestartCount   int
	systemPrompt   string
	description    string
	color          string
	customSections map[string]sidebar.CustomSection

	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr io.ReadCloser
	stdin  io.WriteCloser
	mu     sync.RWMutex

	// Protocol support
	protocol *protocol.ProcessProtocol

	// Persistence
	persistence *AgentPersistence

	// State change notification
	stateChangeNotifier func(agentName string, changeType string, data interface{})

	// Log notification - single entry streaming (no throttling needed)
	lastLogEntry string

	// Early exit detection for startup stability check
	earlyExitChan chan error

	// Last invocation directory for change detection (where user runs 'op' from)
	lastInvocationDir string
}

// MetadataUpdate captures the user-facing metadata for an agent.
type MetadataUpdate struct {
	Description  string
	SystemPrompt string
	Color        string
}

func NewAgent(config AgentConfig, persistence *AgentPersistence) *Agent {
	return &Agent{
		Config:         config,
		Status:         StatusStopped,
		systemPrompt:   strings.TrimSpace(config.SystemPrompt),
		description:    strings.TrimSpace(config.Description),
		color:          strings.TrimSpace(config.Color),
		customSections: make(map[string]sidebar.CustomSection),
		persistence:    persistence,
	}
}

func (a *Agent) Start() error {
	a.mu.Lock()

	if a.Status == StatusRunning {
		a.mu.Unlock()
		return fmt.Errorf("agent %s is already running", a.Config.Name)
	}

	workingDir := strings.TrimSpace(a.Config.ProcessRoot)
	if workingDir == "" || !filepath.IsAbs(workingDir) {
		configDir, err := config.GetConfigDir()
		if err != nil {
			a.mu.Unlock()
			return fmt.Errorf("resolve config directory: %w", err)
		}
		if workingDir == "" {
			workingDir = configDir
		} else {
			workingDir = filepath.Join(configDir, workingDir)
		}
	}

	cmdPath := strings.TrimSpace(a.Config.Command)
	if cmdPath == "" {
		a.mu.Unlock()
		return fmt.Errorf("command is required for agent %s", a.Config.Name)
	}
	if !filepath.IsAbs(cmdPath) && strings.Contains(cmdPath, string(os.PathSeparator)) {
		cmdPath = filepath.Join(workingDir, cmdPath)
	}
	a.cmd = exec.Command(cmdPath, a.Config.Args...)
	a.cmd.Dir = workingDir

	a.cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	a.cmd.Env = os.Environ()
	for key, value := range a.Config.Env {
		a.cmd.Env = append(a.cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	var err error
	a.stdout, err = a.cmd.StdoutPipe()
	if err != nil {
		a.mu.Unlock()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	a.stderr, err = a.cmd.StderrPipe()
	if err != nil {
		a.mu.Unlock()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	a.stdin, err = a.cmd.StdinPipe()
	if err != nil {
		a.mu.Unlock()
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	if err := a.cmd.Start(); err != nil {
		a.mu.Unlock()
		return fmt.Errorf("failed to start process: %w", err)
	}

	a.PID = a.cmd.Process.Pid
	a.Status = StatusRunning
	a.StartTime = time.Now()

	// Create channel for early exit detection
	a.earlyExitChan = make(chan error, 1)

	// Record start in persistence
	if a.persistence != nil {
		a.persistence.RecordStart(a.Config.Name)
		a.persistence.RecordRunning(a.Config.Name)
	}

	// Setup protocol for all processes
	a.setupProtocol()

	go a.waitForExit()

	notifier := a.stateChangeNotifier
	agentName := a.Config.Name

	a.mu.Unlock()

	// Wait 3 seconds to check for early crashes
	fmt.Fprintf(os.Stderr, "[DEBUG %s] Entering 3s stability check\n", agentName)
	startWait := time.Now()
	select {
	case exitErr := <-a.earlyExitChan:
		// Process exited within 3 seconds - this is an error
		// The waitForExit goroutine already updated status and sent notifications
		elapsed := time.Since(startWait)
		fmt.Fprintf(os.Stderr, "[DEBUG %s] Early exit detected after %v, err=%v\n", agentName, elapsed, exitErr)
		if exitErr != nil {
			return fmt.Errorf("agent crashed during startup: %w", exitErr)
		}
		return fmt.Errorf("agent exited during startup")
	case <-time.After(3 * time.Second):
		// Process is still running after 3 seconds - success
		// Notify that agent has successfully started and is stable
		fmt.Fprintf(os.Stderr, "[DEBUG %s] Agent stable after 3 seconds\n", agentName)
		if notifier != nil {
			notifier(agentName, "status", string(StatusRunning))
		}
		return nil
	}
}

func (a *Agent) Stop() error {
	return a.stop(false)
}

func (a *Agent) StopPreservingState() error {
	return a.stop(true)
}

func (a *Agent) stop(preserveRunningState bool) error {
	a.mu.Lock()

	if a.Status != StatusRunning {
		a.mu.Unlock()
		return fmt.Errorf("agent %s is not running", a.Config.Name)
	}

	a.Status = StatusStopping
	cmd := a.cmd
	a.mu.Unlock()

	// Do the blocking operations outside the lock
	if cmd != nil && cmd.Process != nil {
		// Try graceful termination first
		syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)

		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			// Force kill if not terminated
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			select {
			case <-done:
			case <-time.After(1 * time.Second):
			}
		}
	}

	a.mu.Lock()
	a.Status = StatusStopped
	a.PID = 0
	a.customSections = make(map[string]sidebar.CustomSection)

	notifier := a.stateChangeNotifier
	agentName := a.Config.Name
	a.mu.Unlock()

	// Record stop in persistence
	if a.persistence != nil {
		if preserveRunningState {
			// Don't call RecordStop (which sets WasRunning=false)
			// The running state was already captured by SnapshotRunningAgents
		} else {
			a.persistence.RecordStop(agentName)
		}
	}

	// Notify that agent has stopped
	if notifier != nil {
		notifier(agentName, "status", string(StatusStopped))
	}

	return nil
}

func (a *Agent) Restart() error {
	// Check current status
	status := a.GetStatus()

	// Only stop if the agent is running
	if status == StatusRunning {
		if err := a.Stop(); err != nil {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}

	return a.Start()
}

func (a *Agent) GetStatus() ProcessStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Status
}

func (a *Agent) GetLogs() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.persistence != nil {
		return a.persistence.GetLogs(a.Config.Name, 1000) // Get up to 1000 lines
	}

	return []string{}
}

func (a *Agent) addLog(line string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.persistence != nil {
		a.persistence.AddLog(a.Config.Name, line)
	}

	// Send immediate single log entry notification
	a.lastLogEntry = line
	if a.stateChangeNotifier != nil {
		a.stateChangeNotifier(a.Config.Name, "log_entry", line)
	}
}

func (a *Agent) captureOutput(reader io.ReadCloser, prefix string) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := fmt.Sprintf("[%s] %s", prefix, scanner.Text())
		a.addLog(line)
	}
}

func (a *Agent) waitForExit() {
	if a.cmd != nil {
		err := a.cmd.Wait()

		// Stop protocol if it was running
		if a.protocol != nil {
			a.protocol.Stop()
		}

		if err != nil {
			a.addLog(fmt.Sprintf("[error] Agent exited: %v", err))
		}

		a.mu.Lock()

		// Signal early exit if channel exists and is not closed
		earlyExitChan := a.earlyExitChan
		agentName := a.Config.Name

		a.mu.Unlock()

		if earlyExitChan != nil {
			fmt.Fprintf(os.Stderr, "[DEBUG %s waitForExit] Attempting to send to earlyExitChan, err=%v\n", agentName, err)
			select {
			case earlyExitChan <- err:
				// Successfully sent early exit signal
				fmt.Fprintf(os.Stderr, "[DEBUG %s waitForExit] Successfully sent to earlyExitChan\n", agentName)
			default:
				// Channel buffer full or already consumed (after 3s timeout)
				fmt.Fprintf(os.Stderr, "[DEBUG %s waitForExit] Could not send to earlyExitChan (buffer full or timeout)\n", agentName)
			}
		}

		a.mu.Lock()

		if a.Status == StatusRunning {
			var newStatus ProcessStatus
			if err == nil {
				newStatus = StatusStopped // Normal completion, not crashed
				// Record normal stop in persistence
				if a.persistence != nil {
					a.persistence.RecordStop(a.Config.Name)
				}
			} else {
				newStatus = StatusCrashed // Process failed
				// Record crash in persistence
				if a.persistence != nil {
					a.persistence.RecordCrash(a.Config.Name)
				}
			}
			a.Status = newStatus
			a.PID = 0
			notifier := a.stateChangeNotifier
			agentName := a.Config.Name

			// Only auto-restart if it crashed AND auto-restart is enabled
			if a.Status == StatusCrashed && a.Config.AutoRestart && a.RestartCount < a.Config.MaxRestarts {
				a.RestartCount++
				a.mu.Unlock()

				// Notify about the crash before restarting
				if notifier != nil {
					notifier(agentName, "status", string(StatusCrashed))
				}

				time.Sleep(2 * time.Second)
				a.Start()
			} else {
				a.mu.Unlock()

				// Notify about status change
				if notifier != nil {
					notifier(agentName, "status", string(newStatus))
				}
			}
		} else {
			a.Status = StatusStopped
			a.PID = 0
			a.mu.Unlock()
		}

	}
}

// setupProtocol initializes the protocol handler for managed processes
func (a *Agent) setupProtocol() {
	a.protocol = protocol.NewProcessProtocol(a.stdin, a.stdout, a.stderr)

	// Register handlers
	a.protocol.RegisterDefaults(&protocol.DefaultHandlers{
		OnReady: func(pid int, version string) {
			a.mu.Lock()
			a.PID = pid
			a.mu.Unlock()
			a.addLog(fmt.Sprintf("[protocol] Agent ready (PID: %d, Version: %s)", pid, version))
		},
		OnLog: func(level protocol.LogLevel, message string, fields map[string]interface{}) {
			logLine := fmt.Sprintf("[%s] %s", level, message)
			if len(fields) > 0 {
				logLine += fmt.Sprintf(" %v", fields)
			}
			a.addLog(logLine)
		},
		OnEvent: func(name string, data map[string]interface{}) {
			a.addLog(fmt.Sprintf("[event] %s: %v", name, data))
		},
		OnError: func(err string, code int) {
			a.addLog(fmt.Sprintf("[error] %s (code: %d)", err, code))
		},
		OnResponse: func(resp *protocol.ResponseMessage) {
			if resp == nil {
				return
			}
			if resp.Success {
				a.addLog(fmt.Sprintf("[command-response] id=%s success result=%v", resp.CommandID, resp.Result))
			} else {
				a.addLog(fmt.Sprintf("[command-response] id=%s error=%s", resp.CommandID, resp.Error))
			}
		},
		OnSystemPrompt: func(prompt string) {
			trimmed := strings.TrimSpace(prompt)
			a.mu.Lock()
			a.systemPrompt = trimmed
			a.mu.Unlock()
			if trimmed != "" {
				a.addLog("[system-prompt] updated")
			}
			a.notifyMetadataChange()
		},
		OnDescription: func(description string) {
			trimmed := strings.TrimSpace(description)
			a.mu.Lock()
			a.description = trimmed
			a.mu.Unlock()
			if trimmed != "" {
				a.addLog("[description] updated")
			} else {
				a.addLog("[description] cleared")
			}
			a.notifyMetadataChange()
		},
		OnSidebarSection: func(section protocol.SidebarSectionMessage) {
			sectionID := strings.TrimSpace(section.SectionID)
			if sectionID == "" {
				return
			}

			a.mu.Lock()
			a.customSections[sectionID] = sidebar.CustomSection{
				ID:        sectionID,
				Title:     section.Title,
				Content:   section.Content,
				Collapsed: section.Collapsed,
			}
			sections := make([]sidebar.CustomSection, 0, len(a.customSections))
			for _, s := range a.customSections {
				sections = append(sections, s)
			}
			a.mu.Unlock()

			// Notify about sections change
			if a.stateChangeNotifier != nil {
				a.stateChangeNotifier(a.Config.Name, "sections", sections)
			}
		},
		OnCommandRegistry: func(commands []protocol.CommandDescriptor) {
			normalized := protocol.NormalizeCommandDescriptors(commands)
			if len(normalized) == 0 {
				a.addLog("[commands] cleared")
			} else {
				a.addLog(fmt.Sprintf("[commands] registered %d command(s)", len(normalized)))
			}
			if a.stateChangeNotifier != nil {
				out := make([]protocol.CommandDescriptor, len(normalized))
				copy(out, normalized)
				a.stateChangeNotifier(a.Config.Name, "commands", out)
			}
		},
	})

	a.protocol.SetRawOutputHandler(func(line string) {
		a.addLog(fmt.Sprintf("[stdout] %s", line))
	})

	go a.captureOutput(a.stderr, "stderr")

	// Start protocol handler
	a.protocol.Start()

}

func (a *Agent) RegisteredCommands() []protocol.CommandDescriptor {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.protocol == nil {
		return nil
	}

	return a.protocol.RegisteredCommands()
}

// SendCommand sends a command to a managed process and waits for the response
func (a *Agent) SendCommand(ctx context.Context, command string, args map[string]interface{}, workingDir string) (*protocol.ResponseMessage, error) {
	a.mu.RLock()
	status := a.Status
	pro := a.protocol
	a.mu.RUnlock()

	if status != StatusRunning {
		return nil, fmt.Errorf("agent %s is not running", a.Config.Name)
	}
	if pro == nil {
		return nil, fmt.Errorf("protocol not initialized for agent %s", a.Config.Name)
	}

	return pro.SendCommand(ctx, command, args, strings.TrimSpace(workingDir))
}

// SendCommandWithProgress sends a command and surfaces progress events.
func (a *Agent) SendCommandWithProgress(ctx context.Context, command string, args map[string]interface{}, workingDir string, progress func(protocol.CommandProgressMessage)) (*protocol.ResponseMessage, error) {
	a.mu.RLock()
	status := a.Status
	pro := a.protocol
	a.mu.RUnlock()

	if status != StatusRunning {
		return nil, fmt.Errorf("agent %s is not running", a.Config.Name)
	}
	if pro == nil {
		return nil, fmt.Errorf("protocol not initialized for agent %s", a.Config.Name)
	}

	return pro.SendCommandWithProgress(ctx, command, args, strings.TrimSpace(workingDir), progress)
}

// SendLifecycleEvent sends a lifecycle event to the agent subprocess
func (a *Agent) SendLifecycleEvent(eventType string, data map[string]interface{}) error {
	a.mu.RLock()
	pro := a.protocol
	a.mu.RUnlock()

	if pro == nil {
		return fmt.Errorf("protocol not initialized for agent %s", a.Config.Name)
	}

	return pro.SendLifecycleEvent(eventType, data)
}

// CheckAndNotifyInvocationDirChange checks if the invocation directory has changed and notifies the agent
func (a *Agent) CheckAndNotifyInvocationDirChange(newInvocationDir string) error {
	trimmed := strings.TrimSpace(newInvocationDir)
	if trimmed == "" {
		return nil
	}

	// Get absolute path for comparison
	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		absPath = trimmed
	}

	a.mu.Lock()
	oldPath := a.lastInvocationDir

	// If this is the first invocation directory, just store it
	if oldPath == "" {
		a.lastInvocationDir = absPath
		a.mu.Unlock()
		return nil
	}

	// Check if directory changed
	if oldPath != absPath {
		a.lastInvocationDir = absPath
		a.mu.Unlock()

		// Send lifecycle event
		return a.SendLifecycleEvent("invocation_directory_changed", map[string]interface{}{
			"old_path": oldPath,
			"new_path": absPath,
		})
	}

	a.mu.Unlock()
	return nil
}

func (a *Agent) SystemPrompt() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if trimmed := strings.TrimSpace(a.systemPrompt); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(a.Config.SystemPrompt)
}

func (a *Agent) Description() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if trimmed := strings.TrimSpace(a.description); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(a.Config.Description)
}

func (a *Agent) CustomSections() []sidebar.CustomSection {
	a.mu.RLock()
	defer a.mu.RUnlock()

	sections := make([]sidebar.CustomSection, 0, len(a.customSections))
	for _, section := range a.customSections {
		sections = append(sections, section)
	}
	return sections
}

func (a *Agent) Color() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if trimmed := strings.TrimSpace(a.color); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(a.Config.Color)
}

func (a *Agent) metadataSnapshot() MetadataUpdate {
	a.mu.RLock()
	systemPrompt := a.systemPrompt
	description := a.description
	color := a.color
	a.mu.RUnlock()

	desc := strings.TrimSpace(description)
	if desc == "" {
		desc = strings.TrimSpace(a.Config.Description)
	}
	prompt := strings.TrimSpace(systemPrompt)
	if prompt == "" {
		prompt = strings.TrimSpace(a.Config.SystemPrompt)
	}
	col := strings.TrimSpace(color)
	if col == "" {
		col = strings.TrimSpace(a.Config.Color)
	}
	return MetadataUpdate{
		Description:  desc,
		SystemPrompt: prompt,
		Color:        col,
	}
}

func (a *Agent) notifyMetadataChange() {
	if a.stateChangeNotifier == nil {
		return
	}
	meta := a.metadataSnapshot()
	a.stateChangeNotifier(a.Config.Name, "metadata", meta)
}
