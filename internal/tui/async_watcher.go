package tui

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"

	"tui/internal/protocol"
	llm "tui/llm"
	tooling "tui/tools"
	tooltypes "tui/tools/types"
	toolstate "tui/toolstate"
	"tui/util"
)

type AsyncTaskInfo struct {
	ID       string
	ToolName string
	Status   string
	Label    string
}

type TaskUpdateMsg struct {
	Tasks []AsyncTaskInfo
}

// AsyncTaskWatcher watches for task updates and provides real-time task list
type AsyncTaskWatcher struct {
	mu      sync.RWMutex
	tasks   map[string]AsyncTaskInfo // keyed by task ID
	updates chan TaskUpdateMsg
	cancel  context.CancelFunc
	db      *sql.DB
}

func NewAsyncTaskWatcher(db *sql.DB) *AsyncTaskWatcher {
	return &AsyncTaskWatcher{
		tasks:   make(map[string]AsyncTaskInfo),
		updates: make(chan TaskUpdateMsg, 16),
		db:      db,
	}
}

// Start begins watching for task updates
func (w *AsyncTaskWatcher) Start(ctx context.Context) {
	if w == nil {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	// Initial load from database
	w.loadFromDB()

	go w.streamLoop(ctx)
}

// Stop stops the watcher
func (w *AsyncTaskWatcher) Stop() {
	if w != nil && w.cancel != nil {
		w.cancel()
	}
}

func (w *AsyncTaskWatcher) GetActiveTasks() []AsyncTaskInfo {
	if w == nil {
		return nil
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	tasks := make([]AsyncTaskInfo, 0, len(w.tasks))
	for _, task := range w.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}

func (w *AsyncTaskWatcher) Updates() <-chan TaskUpdateMsg {
	if w == nil {
		return nil
	}
	return w.updates
}

// loadFromDB loads active tasks from the database
func (w *AsyncTaskWatcher) loadFromDB() {
	if w == nil || w.db == nil {
		return
	}

	query := `SELECT id, tool_name, status, metadata, call_id
	          FROM tool_tasks
	          WHERE status IN ('pending', 'loading')
	          ORDER BY created_at DESC
	          LIMIT 50`

	rows, err := w.db.Query(query)
	if err != nil {
		return
	}
	defer rows.Close()

	w.mu.Lock()
	defer w.mu.Unlock()

	w.tasks = make(map[string]AsyncTaskInfo)

	for rows.Next() {
		var id, toolName, status, metadata, callID sql.NullString
		if err := rows.Scan(&id, &toolName, &status, &metadata, &callID); err != nil {
			continue
		}

		// Extract label from metadata
		label := toolName.String
		if metadata.Valid && metadata.String != "" {
			var meta map[string]interface{}
			if err := json.Unmarshal([]byte(metadata.String), &meta); err == nil {
				if labelStr, ok := meta["label"].(string); ok && labelStr != "" {
					label = labelStr
				}
			}
		}

		info := AsyncTaskInfo{
			ID:       id.String,
			ToolName: toolName.String,
			Status:   status.String,
			Label:    strings.TrimSpace(label),
		}

		w.tasks[id.String] = info
	}

	w.sendUpdate()
}

// streamLoop subscribes to task events from the daemon
func (w *AsyncTaskWatcher) streamLoop(ctx context.Context) {
	if w == nil {
		return
	}

	// Keep retrying connection to daemon
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Connect to daemon task stream
		payload := struct {
			Type string `json:"type"`
		}{Type: "watch_all_tasks"}

		conn, cleanup, err := tooling.OpenStream(ctx, payload)
		if err != nil {
			// Wait before retrying
			time.Sleep(2 * time.Second)
			continue
		}

		// Read events from stream
		scanner := bufio.NewScanner(conn)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 64*1024*1024)

		// Read initial success response
		if !scanner.Scan() {
			cleanup()
			time.Sleep(2 * time.Second)
			continue
		}

		var resp struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil || !resp.Success {
			cleanup()
			time.Sleep(2 * time.Second)
			continue
		}

		for scanner.Scan() {
			var event struct {
				Type string `json:"type"`
				Task *struct {
					ID       string `json:"id"`
					ToolName string `json:"tool_name"`
					Status   string `json:"status"`
					Metadata string `json:"metadata"`
				} `json:"task"`
			}

			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				continue
			}

			w.handleTaskEvent(event.Type, event.Task)
		}

		cleanup()

		// Connection lost, retry after delay
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// handleTaskEvent processes a task event from the stream
func (w *AsyncTaskWatcher) handleTaskEvent(eventType string, task *struct {
	ID       string `json:"id"`
	ToolName string `json:"tool_name"`
	Status   string `json:"status"`
	Metadata string `json:"metadata"`
}) {
	if w == nil || task == nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	taskID := strings.TrimSpace(task.ID)
	eventKind := strings.ToLower(strings.TrimSpace(eventType))
	status := strings.ToLower(strings.TrimSpace(task.Status))

	shouldRemove := status == "complete" || status == "failed" ||
		eventKind == "completed" || eventKind == "failed" || eventKind == "deleted"

	if status != "loading" && status != "pending" {
		shouldRemove = true
	}

	if shouldRemove {
		if _, exists := w.tasks[taskID]; exists {
			delete(w.tasks, taskID)
			w.sendUpdate()
		}
		return
	}

	// Extract label from metadata
	label := task.ToolName
	if task.Metadata != "" {
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(task.Metadata), &meta); err == nil {
			if labelStr, ok := meta["label"].(string); ok && labelStr != "" {
				label = labelStr
			}
		}
	}

	info := AsyncTaskInfo{
		ID:       taskID,
		ToolName: task.ToolName,
		Status:   task.Status,
		Label:    strings.TrimSpace(label),
	}

	oldInfo, exists := w.tasks[taskID]
	if !exists || !asyncTaskInfoEqual(oldInfo, info) {
		w.tasks[taskID] = info
		w.sendUpdate()
	}
}

// sendUpdate sends an update notification
func (w *AsyncTaskWatcher) sendUpdate() {
	tasks := make([]AsyncTaskInfo, 0, len(w.tasks))
	for _, task := range w.tasks {
		tasks = append(tasks, task)
	}

	sort.SliceStable(tasks, func(i, j int) bool {
		statusI := strings.ToLower(strings.TrimSpace(tasks[i].Status))
		statusJ := strings.ToLower(strings.TrimSpace(tasks[j].Status))
		if statusI != statusJ {
			return statusI < statusJ
		}
		labelI := strings.ToLower(strings.TrimSpace(tasks[i].Label))
		labelJ := strings.ToLower(strings.TrimSpace(tasks[j].Label))
		if labelI != labelJ {
			return labelI < labelJ
		}
		return strings.ToLower(strings.TrimSpace(tasks[i].ID)) < strings.ToLower(strings.TrimSpace(tasks[j].ID))
	})

	select {
	case w.updates <- TaskUpdateMsg{Tasks: tasks}:
	default:
	}
}

func asyncTaskInfoEqual(a, b AsyncTaskInfo) bool {
	if strings.TrimSpace(a.ID) != strings.TrimSpace(b.ID) {
		return false
	}
	if strings.TrimSpace(a.ToolName) != strings.TrimSpace(b.ToolName) {
		return false
	}
	if strings.TrimSpace(a.Status) != strings.TrimSpace(b.Status) {
		return false
	}
	if strings.TrimSpace(a.Label) != strings.TrimSpace(b.Label) {
		return false
	}
	return true
}

// Model methods for async task handling
// These methods are part of Model but organized here with async watcher logic

func (m *Model) handleAsyncTasksSnapshot(msg asyncTasksSnapshotMsg) tea.Cmd {
	var cmds []tea.Cmd
	if msg.err != nil {
		cmds = append(cmds, util.ReportWarn(fmt.Sprintf("Failed to load async tasks: %v", msg.err)))
	} else if len(msg.tasks) > 0 {
		if m.llmEngine != nil {
			m.llmEngine.RestoreAsyncTasks(msg.tasks)
		}
		sessionID := strings.TrimSpace(m.sessionID)
		pending := 0
		completed := 0
		resumeCallSet := make(map[string]struct{})
		for _, task := range msg.tasks {
			status := strings.ToLower(strings.TrimSpace(task.Status))
			taskSession := strings.TrimSpace(task.SessionID)
			callID := strings.TrimSpace(task.CallID)
			sameSession := sessionID != "" && taskSession == sessionID
			callName := asyncTaskCallName(task)
			displayLabel := asyncTaskDisplayLabel(task)

			switch status {
			case "loading", "pending":
				pending++
				if callID == "" {
					continue
				}
				displayStatus := strings.TrimSpace(task.Status)
				if displayStatus == "" {
					displayStatus = "pending"
				}
				initialLine := fmt.Sprintf("async task %s scheduled (%s)", strings.TrimSpace(task.ID), strings.ToLower(displayStatus))
				call := tooltypes.Call{ID: callID, Name: callName, Input: initialLine, Finished: false}
				m.sessionManager().EnsureToolCall(context.Background(), taskSession, call)
				m.streamManager().TrackToolCall(taskSession, call)
				snapshotFinished := false
				snapshotLabel := displayLabel
				if sameSession && m.messages != nil {
					if cmd := m.messages.EnsureToolCall(call); cmd != nil {
						cmds = append(cmds, cmd)
					}
					if displayLabel != "" {
						if cmd := m.messages.SetToolDisplay(callID, toolstate.ExecutionDisplay{Label: displayLabel}); cmd != nil {
							cmds = append(cmds, cmd)
						}
					}
					if cmd := m.messages.SetToolFlags(callID, asyncFlagsForTask(task)); cmd != nil {
						cmds = append(cmds, cmd)
					}
					if lifecycle := lifecycleFromAsyncStatus(task.Status, false); lifecycle != toolstate.LifecycleUnknown {
						if cmd := m.messages.SetToolLifecycle(callID, lifecycle); cmd != nil {
							cmds = append(cmds, cmd)
						}
					}
					if len(task.Progress) > 0 {
						if cmd := m.messages.SetToolProgress(callID, progressEntriesFromAsync(task.Progress)); cmd != nil {
							cmds = append(cmds, cmd)
						}
						m.asyncProgressSeen[callID] = len(task.Progress)
						if cmd := m.refreshToolDetail(callID); cmd != nil {
							cmds = append(cmds, cmd)
						}
					} else {
						delete(m.asyncProgressSeen, callID)
					}
					m.messages.UpdateToolResultMeta(callID, func(meta map[string]any) map[string]any {
						if meta == nil {
							meta = make(map[string]any)
						}
						meta["async_task_id"] = task.ID
						meta["async_task_status"] = task.Status
						if task.CompletedAt != nil {
							meta["async_task_completed_at"] = task.CompletedAt.Format(time.RFC3339)
						}
						if trimmedArgs := strings.TrimSpace(task.Args); trimmedArgs != "" {
							meta["async_task_args"] = trimmedArgs
						}
						return meta
					})
				}
				if snapshotLabel != "" {
					tooling.UpdateAsyncProgressSnapshot(callID, snapshotLabel, nil, snapshotFinished)
				}
				// Register restored slash command async tasks for completion tracking
				if trimmedTaskID := strings.TrimSpace(task.ID); trimmedTaskID != "" {
					m.pendingAsyncTasks[trimmedTaskID] = callID
				}
			case "complete", "failed":
				if callID == "" {
					continue
				}
				call := tooltypes.Call{ID: callID, Name: callName, Finished: true}
				m.sessionManager().EnsureToolCall(context.Background(), taskSession, call)
				if !sameSession || m.messages == nil {
					continue
				}
				m.sessionManager().EnsureToolCall(context.Background(), sessionID, call)
				m.messages.UpdateToolResultMeta(callID, func(meta map[string]any) map[string]any {
					if meta == nil {
						meta = make(map[string]any)
					}
					meta["async_task_id"] = task.ID
					meta["async_task_status"] = task.Status
					if task.CompletedAt != nil {
						meta["async_task_completed_at"] = task.CompletedAt.Format(time.RFC3339)
					}
					if trimmed := strings.TrimSpace(task.Metadata); trimmed != "" {
						meta["async_task_metadata"] = trimmed
					}
					if trimmedArgs := strings.TrimSpace(task.Args); trimmedArgs != "" {
						meta["async_task_args"] = trimmedArgs
					}
					return meta
				})
				delete(m.asyncProgressSeen, callID)
				if len(task.Progress) > 0 {
					m.messages.SetToolProgress(callID, progressEntriesFromAsync(task.Progress))
				}
				m.messages.SetToolFlags(callID, asyncFlagsForTask(task))
				if lifecycle := lifecycleFromAsyncStatus(task.Status, status == "failed"); lifecycle != toolstate.LifecycleUnknown {
					m.messages.SetToolLifecycle(callID, lifecycle)
				}
				if displayLabel != "" {
					m.messages.SetToolDisplay(callID, toolstate.ExecutionDisplay{Label: displayLabel})
				}

				resultContent := strings.TrimSpace(task.Result)
				if resultContent == "" {
					resultContent = strings.TrimSpace(task.Error)
				}
				if resultContent == "" {
					if status == "complete" {
						resultContent = "async task completed"
					} else {
						resultContent = "async task failed"
					}
				}

				result := tooltypes.Result{
					ToolCallID: callID,
					Name:       callName,
					Content:    resultContent,
					Metadata:   buildAsyncTaskMetadata(task),
					IsError:    status == "failed",
				}
				m.streamManager().ClearToolCall(sessionID, callID)
				m.messages.FinishTool(callID, result)
				if cmd := m.recordToolResultsForSession(sessionID, []tooltypes.Result{result}); cmd != nil {
					cmds = append(cmds, cmd)
				}
				handled := m.sessionManager().ToolResultHandled(context.Background(), sessionID, callID)
				if !handled {
					completed++
					resumeCallSet[callID] = struct{}{}
				}
				if displayLabel != "" {
					tooling.UpdateAsyncProgressSnapshot(callID, displayLabel, nil, true)
				}
			}
		}
		if pending > 0 {
			// cmds = append(cmds, util.ReportInfo(fmt.Sprintf("Restored %d pending async task(s)", pending)))
		}
		if completed > 0 {
			// cmds = append(cmds, util.ReportInfo(fmt.Sprintf("Finalized %d async task(s)", completed)))
			if len(resumeCallSet) > 0 {
				if resumeCmd := m.autoResumeAfterAsyncResult(sessionID); resumeCmd != nil {
					cmds = append(cmds, resumeCmd)
				}
				for id := range resumeCallSet {
					m.sessionManager().MarkToolResultHandled(context.Background(), sessionID, id)
				}
			}
		}
	} else {
		m.asyncProgressSeen = make(map[string]int)
	}
	return batchCmds(cmds)
}

func (m *Model) applyProgressEntries(callID string, entries []tooling.AsyncTaskProgress) bool {
	if m == nil || m.messages == nil || callID == "" || len(entries) == 0 {
		return false
	}
	progress := progressEntriesFromAsync(entries)
	if len(progress) == 0 {
		return false
	}
	m.messages.AppendToolProgress(callID, progress)
	return true
}

func (m *Model) diffAndApplyProgress(callID string, all []tooling.AsyncTaskProgress) bool {
	trimmedID := strings.TrimSpace(callID)
	if trimmedID == "" {
		return false
	}
	if len(all) == 0 {
		delete(m.asyncProgressSeen, trimmedID)
		return false
	}
	seen := m.asyncProgressSeen[trimmedID]
	if seen < 0 || seen > len(all) {
		seen = 0
	}
	if seen == len(all) {
		return false
	}
	newEntries := append([]tooling.AsyncTaskProgress(nil), all[seen:]...)
	changed := m.applyProgressEntries(trimmedID, newEntries)
	m.asyncProgressSeen[trimmedID] = len(all)
	return changed
}

func (m *Model) incrementProgressCounter(callID string, count int) {
	trimmedID := strings.TrimSpace(callID)
	if trimmedID == "" || count <= 0 {
		return
	}
	m.asyncProgressSeen[trimmedID] = m.asyncProgressSeen[trimmedID] + count
}

func (m *Model) handleAsyncToolUpdate(msg llm.AsyncToolUpdateMsg) tea.Cmd {
	var cmds []tea.Cmd
	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" {
		sessionID = m.sessionID
	}
	callID := strings.TrimSpace(msg.CallID)
	if callID != "" {
		toolName := ""
		finished := false
		if msg.Task != nil {
			toolName = strings.TrimSpace(msg.Task.ToolName)
			status := strings.ToLower(strings.TrimSpace(msg.Task.Status))
			finished = status == "complete" || status == "failed"
		}
		if msg.Result != nil {
			_, _, _, metaTool := extractAsyncTaskMetadata(msg.Result.Metadata)
			toolName = firstNonEmpty(strings.TrimSpace(metaTool), strings.TrimSpace(msg.Result.Name), toolName)
			finished = finished || asyncResultAppearsComplete(*msg.Result)
		}
		if toolName == "" && msg.Task != nil {
			_, _, _, metaTool := extractAsyncTaskMetadata(msg.Task.Metadata)
			toolName = firstNonEmpty(strings.TrimSpace(metaTool), strings.TrimSpace(msg.Task.ToolName))
		}
		if toolName == "" {
			toolName = tooling.AsyncToolName
		}
		existed := m.sessionManager().ToolCallExists(context.Background(), sessionID, callID)
		call := tooltypes.Call{ID: callID, Name: toolName, Finished: finished}
		m.sessionManager().EnsureToolCall(context.Background(), sessionID, call)
		if !existed {
			m.recordAssistantToolCallsForSession(sessionID, []tooltypes.Call{call}, "")
		}
	}
	if callID != "" && sessionID == m.sessionID && m.messages != nil {
		progressUpdated := false
		if len(msg.Progress) > 0 {
			m.incrementProgressCounter(callID, len(msg.Progress))
			if m.applyProgressEntries(callID, msg.Progress) {
				progressUpdated = true
			}
			if cmd := m.messages.SetToolLifecycle(callID, toolstate.LifecycleRunning); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if task := msg.Task; task != nil {
			m.asyncProgressSeen[callID] = len(task.Progress)
			if cmd := m.messages.SetToolFlags(callID, asyncFlagsForTask(*task)); cmd != nil {
				cmds = append(cmds, cmd)
			}
			if lifecycle := lifecycleFromAsyncStatus(task.Status, false); lifecycle != toolstate.LifecycleUnknown {
				if cmd := m.messages.SetToolLifecycle(callID, lifecycle); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			if len(task.Progress) > 0 {
				if cmd := m.messages.SetToolProgress(callID, progressEntriesFromAsync(task.Progress)); cmd != nil {
					cmds = append(cmds, cmd)
				}
				progressUpdated = true
			}
			m.messages.UpdateToolResultMeta(callID, func(meta map[string]any) map[string]any {
				if meta == nil {
					meta = make(map[string]any)
				}
				meta["async_task_id"] = task.ID
				meta["async_task_status"] = task.Status
				if task.CompletedAt != nil {
					meta["async_task_completed_at"] = task.CompletedAt.Format(time.RFC3339)
				}
				if trimmed := strings.TrimSpace(task.Metadata); trimmed != "" {
					meta["async_task_metadata"] = trimmed
				}
				if trimmedArgs := strings.TrimSpace(task.Args); trimmedArgs != "" {
					meta["async_task_args"] = trimmedArgs
				}
				return meta
			})
		}
		if progressUpdated {
			if cmd := m.refreshToolDetail(callID); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	var resumeCallIDs []string
	if msg.Result != nil {
		result := *msg.Result
		callID := strings.TrimSpace(result.ToolCallID)
		m.streamManager().ClearToolCall(sessionID, result.ToolCallID)
		delete(m.asyncProgressSeen, callID)
		if sessionID == m.sessionID && m.messages != nil {
			m.messages.SetToolFlags(result.ToolCallID, toolstate.ExecutionFlags{Async: true})
			if result.IsError {
				m.messages.SetToolLifecycle(result.ToolCallID, toolstate.LifecycleFailed)
			} else {
				m.messages.SetToolLifecycle(result.ToolCallID, toolstate.LifecycleCompleted)
			}
			m.messages.FinishTool(result.ToolCallID, result)
			if cmd := m.refreshToolDetail(result.ToolCallID); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if cmd := m.recordToolResultsForSession(sessionID, []tooltypes.Result{result}); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if callID == "" || !m.sessionManager().ToolResultHandled(context.Background(), sessionID, callID) {
			if callID != "" {
				resumeCallIDs = append(resumeCallIDs, callID)
			}
		}
	} else if strings.TrimSpace(msg.Error) != "" && sessionID == m.sessionID {
		delete(m.asyncProgressSeen, callID)
		cmds = append(cmds, util.ReportWarn(fmt.Sprintf("Async task error: %s", msg.Error)))
	}
	if len(resumeCallIDs) > 0 {
		// Mark results as handled BEFORE triggering resume to ensure the marks are persisted
		// before the LLM stream saves its response
		for _, id := range resumeCallIDs {
			m.sessionManager().MarkToolResultHandled(context.Background(), sessionID, id)
		}
		if resumeCmd := m.autoResumeAfterAsyncResult(sessionID); resumeCmd != nil {
			cmds = append(cmds, resumeCmd)
		}
	}
	if next := m.waitAsyncTaskUpdate(); next != nil {
		cmds = append(cmds, next)
	}
	return batchCmds(cmds)
}

func (m *Model) scheduleAsyncAgentCommand(agentName string, desc protocol.CommandDescriptor, args map[string]any) tea.Cmd {
	sessionID := strings.TrimSpace(m.sessionID)
	if sessionID == "" {
		return util.ReportWarn("No active session")
	}
	payloadArgs := args
	if payloadArgs == nil {
		payloadArgs = map[string]any{}
	}
	trimmedCommandName := strings.TrimSpace(desc.Name)
	payload := map[string]any{
		"tool":    trimmedCommandName,
		"input":   payloadArgs,
		"mode":    "agent",
		"agent":   strings.TrimSpace(agentName),
		"command": trimmedCommandName,
	}
	if len(payloadArgs) > 0 {
		payload["command_args"] = payloadArgs
	}
	if progressLabel := strings.TrimSpace(desc.ProgressLabel); progressLabel != "" {
		payload["progress_label"] = progressLabel
	}
	commandLabel := strings.TrimSpace(desc.Title)
	if commandLabel == "" {
		commandLabel = trimmedCommandName
	}
	if commandLabel != "" {
		payload["command_label"] = commandLabel
	}
	payload["origin"] = "tui"
	if sessionID != "" {
		payload["client_id"] = sessionID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return util.ReportError(fmt.Errorf("schedule async command: %w", err))
	}
	callID := generateAsyncCallID()
	callName := strings.TrimSpace(desc.Name)
	if callName == "" {
		callName = tooling.AsyncToolName
	}
	call := tooltypes.Call{
		ID:       callID,
		Name:     callName,
		Input:    string(body),
		Finished: false,
		Reason:   strings.TrimSpace(desc.Description),
	}
	m.streamManager().TrackToolCall(sessionID, call)
	m.sessionManager().EnsureToolCall(context.Background(), sessionID, call)
	var cmds []tea.Cmd
	if m.messages != nil {
		if cmd := m.messages.EnsureToolCall(call); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.messages.SetToolFlags(callID, toolstate.ExecutionFlags{Async: true}); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.messages.SetToolLifecycle(callID, toolstate.LifecyclePending); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	ctx := tooling.WithSessionContext(context.Background(), sessionID, callID)
	delete(m.asyncProgressSeen, callID)
	content, metadata := asyncToolRunner(ctx, string(body), m.workingDir, sessionID, callID)
	if m.messages != nil {
		updated := false
		if trimmed := strings.TrimSpace(content); trimmed != "" {
			delta := trimmed
			if !strings.HasSuffix(delta, "\n") {
				delta += "\n"
			}
			m.messages.UpdateToolDelta(callID, "", delta)
			updated = true
		}
		if trimmedMeta := strings.TrimSpace(metadata); trimmedMeta != "" {
			m.messages.UpdateToolResultMeta(callID, func(meta map[string]any) map[string]any {
				if meta == nil {
					meta = make(map[string]any)
				}
				meta["async_metadata"] = trimmedMeta
				return meta
			})
			updated = true
		}
		if updated {
			if cmd := m.refreshToolDetail(callID); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	placeholderContent := strings.TrimSpace(content)
	if placeholderContent == "" {
		placeholderContent = "async task pending"
	}
	m.recordAssistantToolCallsForSession(sessionID, []tooltypes.Call{call}, placeholderContent)
	placeholderResult := tooltypes.Result{
		ToolCallID: callID,
		Name:       callName,
		Content:    placeholderContent,
		Metadata:   metadata,
	}
	if cmd := m.recordToolResultsForSession(sessionID, []tooltypes.Result{placeholderResult}); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if taskID, _, _, metaTool := extractAsyncTaskMetadata(metadata); taskID != "" {
		if task, err := tooling.FetchAsyncTask(context.Background(), taskID); err == nil && task != nil {
			m.llmEngine.RestoreAsyncTasks([]tooling.AsyncTask{*task})
		} else {
			toolName := callName
			if trimmed := strings.TrimSpace(metaTool); trimmed != "" {
				toolName = trimmed
			}
			m.llmEngine.RestoreAsyncTasks([]tooling.AsyncTask{{
				ID:          taskID,
				SessionID:   sessionID,
				CallID:      callID,
				ToolName:    toolName,
				Mode:        "agent",
				AgentName:   strings.TrimSpace(agentName),
				CommandName: strings.TrimSpace(desc.Name),
			}})
		}
	}
	if next := m.waitAsyncTaskUpdate(); next != nil {
		cmds = append(cmds, next)
	}
	return batchCmds(cmds)
}

// isTaskComplete checks if an async task has completed (successfully or with error)
func isTaskComplete(task *tooling.AsyncTask) bool {
	if task == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(task.Status))
	return status == "complete" || status == "failed"
}

// isTaskFailed checks if an async task ended in failure
func isTaskFailed(task *tooling.AsyncTask) bool {
	if task == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(task.Status))
	return status == "failed"
}

// handleSlashCommandAsyncCompletion handles completion of async tasks from slash commands
// It detects which tasks were removed (completed) and finishes their tool calls,
// then re-triggers the LLM if needed
func (m *Model) handleSlashCommandAsyncCompletion(msg TaskUpdateMsg) tea.Cmd {
	if m == nil || m.messages == nil || len(m.pendingAsyncTasks) == 0 {
		return nil
	}

	var completedTasks []struct {
		taskID string
		callID string
		task   *tooling.AsyncTask
	}

	// Detect which pending tasks have been completed by checking their actual status in the daemon
	// Don't rely solely on tasks disappearing from the watcher (they might get stuck)
	for taskID, callID := range m.pendingAsyncTasks {
		if strings.TrimSpace(callID) == "" || strings.TrimSpace(taskID) == "" {
			continue
		}

		// Fetch the actual task status from the daemon
		task, err := tooling.FetchAsyncTask(context.Background(), taskID)
		if err != nil {
			// If task can't be fetched, it might be deleted - remove from tracking
			delete(m.pendingAsyncTasks, taskID)
			continue
		}

		// Check if task has actually completed (not just removed from watcher)
		if isTaskComplete(task) {
			completedTasks = append(completedTasks, struct {
				taskID string
				callID string
				task   *tooling.AsyncTask
			}{taskID, callID, task})
		}
	}

	if len(completedTasks) == 0 {
		return nil
	}

	var cmds []tea.Cmd

	// Process each completed task
	for _, ct := range completedTasks {
		callID := strings.TrimSpace(ct.callID)
		task := ct.task
		if task == nil {
			delete(m.pendingAsyncTasks, ct.taskID)
			continue
		}

		// Determine result content and error status
		resultContent := strings.TrimSpace(task.Result)
		isError := isTaskFailed(task)

		if isError && resultContent == "" {
			resultContent = strings.TrimSpace(task.Error)
		}
		if resultContent == "" {
			if isError {
				resultContent = "async task failed"
			} else {
				resultContent = "async task completed"
			}
		}

		// Build metadata from task
		metadata := buildAsyncTaskMetadata(*task)

		// Finish the tool call with the result
		result := tooltypes.Result{
			ToolCallID: callID,
			Name:       strings.TrimSpace(task.ToolName),
			Content:    resultContent,
			Metadata:   metadata,
			IsError:    isError,
			Pending:    false,
		}

		// Clear the tool call from pending list so it doesn't block LLM resume
		// This must be done before autoResumeAfterAsyncResult to allow resumption
		m.streamManager().ClearToolCall(m.sessionID, callID)

		m.messages.FinishTool(callID, result)
		if cmd := m.recordToolResultsForSession(m.sessionID, []tooltypes.Result{result}); cmd != nil {
			cmds = append(cmds, cmd)
		}

		// Mark as handled and remove from pending
		delete(m.pendingAsyncTasks, ct.taskID)

		// Re-trigger LLM with the tool result (similar to how agents handle async tool results)
		// Check if this task should trigger LLM resume
		handled := m.sessionManager().ToolResultHandled(context.Background(), m.sessionID, callID)
		if !handled {
			// Mark as handled BEFORE triggering resume to ensure the mark is persisted
			// before the LLM stream saves its response
			m.sessionManager().MarkToolResultHandled(context.Background(), m.sessionID, callID)
			// Trigger auto-resume for LLM to continue after async tool completes
			if resumeCmd := m.autoResumeAfterAsyncResult(m.sessionID); resumeCmd != nil {
				cmds = append(cmds, resumeCmd)
			}
		}
	}

	return batchCmds(cmds)
}
