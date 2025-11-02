package llm

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	tooling "tui/tools"
	tooltypes "tui/tools/types"
)

type asyncTaskRegistration struct {
	TaskID    string
	SessionID string
	CallID    string
	ToolName  string
	Daemon    string // Which daemon this task is running on
}

type asyncTracker struct {
	mu       sync.Mutex
	watching map[string]*watchState
	updates  chan AsyncToolUpdateMsg
}

type watchState struct {
	registration asyncTaskRegistration
	seen         int
}

func newAsyncTracker() *asyncTracker {
	return &asyncTracker{
		watching: make(map[string]*watchState),
		updates:  make(chan AsyncToolUpdateMsg, 64),
	}
}

func (a *asyncTracker) Updates() <-chan AsyncToolUpdateMsg {
	if a == nil {
		return nil
	}
	return a.updates
}

func (a *asyncTracker) Watch(reg asyncTaskRegistration) {
	if a == nil || strings.TrimSpace(reg.TaskID) == "" {
		return
	}
	a.mu.Lock()
	if _, exists := a.watching[reg.TaskID]; exists {
		a.mu.Unlock()
		return
	}
	state := &watchState{registration: reg}
	a.watching[reg.TaskID] = state
	a.mu.Unlock()

	go a.monitor(state)
}

func (a *asyncTracker) Restore(tasks []tooling.AsyncTask) {
	if a == nil {
		return
	}
	for _, task := range tasks {
		status := strings.ToLower(strings.TrimSpace(task.Status))
		if status == "complete" || status == "failed" {
			continue
		}
		a.mu.Lock()
		_, exists := a.watching[task.ID]
		if !exists {
			state := &watchState{registration: asyncTaskRegistration{
				TaskID:    task.ID,
				SessionID: task.SessionID,
				CallID:    task.CallID,
				ToolName:  task.ToolName,
				Daemon:    task.Daemon,
			}, seen: len(task.Progress)}
			a.watching[task.ID] = state
			go a.monitor(state)
		}
		a.mu.Unlock()
	}
}

func (a *asyncTracker) monitor(state *watchState) {
	defer func() {
		a.mu.Lock()
		if state != nil {
			delete(a.watching, state.registration.TaskID)
		}
		a.mu.Unlock()
	}()
	if a.stream(state) {
		return
	}
	a.poll(state)
}

func (a *asyncTracker) stream(state *watchState) bool {
	if state == nil {
		return false
	}
	reg := state.registration
	// Watch the task on the correct daemon
	daemonName := reg.Daemon
	if daemonName == "" {
		daemonName = "local"
	}
	events, cancel, err := tooling.WatchAsyncTaskOnDaemon(context.Background(), reg.TaskID, daemonName)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return false
	}
	defer cancel()
	for ev := range events {
		if !a.handleStreamEvent(state, ev) {
			// Event handler returned false, indicating task completed/failed
			return true
		}
	}
	// Stream closed without handling a completion event
	// This can happen if the task completed before the stream opened
	// Return false so polling can detect the final status
	return false
}

func (a *asyncTracker) handleStreamEvent(state *watchState, ev tooling.AsyncTaskEvent) bool {
	if state == nil {
		return false
	}
	reg := state.registration
	task := ev.Task
	regToolName := strings.TrimSpace(reg.ToolName)
	if task != nil {
		enriched := mergeMetadataWithProgress(task.Metadata, task.Progress)
		if enriched != "" {
			task.Metadata = enriched
		}
		state.seen = len(task.Progress)
		if tooling.IsAgentCommandToolName(regToolName) && regToolName != "" {
			task.ToolName = regToolName
		}
	}
	eventType := strings.ToLower(strings.TrimSpace(string(ev.Type)))
	switch eventType {
	case "", string(tooling.AsyncTaskEventSnapshot):
		if task != nil {
			a.emit(AsyncToolUpdateMsg{SessionID: reg.SessionID, CallID: reg.CallID, Task: task})
		}
		return true
	case string(tooling.AsyncTaskEventProgress):
		progress := []tooling.AsyncTaskProgress(nil)
		if ev.Progress != nil {
			progress = append(progress, *ev.Progress)
		}
		a.emit(AsyncToolUpdateMsg{SessionID: reg.SessionID, CallID: reg.CallID, Task: task, Progress: progress})
		return true
	case string(tooling.AsyncTaskEventCompleted):
		if task == nil {
			return false
		}
		name := firstNonEmpty(strings.TrimSpace(task.ToolName), regToolName)
		if tooling.IsAgentCommandToolName(regToolName) && regToolName != "" {
			name = regToolName
		}
		res := tooltypes.Result{
			ToolCallID: strings.TrimSpace(reg.CallID),
			Name:       name,
			Metadata:   mergeMetadataWithProgress(task.Metadata, task.Progress),
			Pending:    false,
		}
		content := strings.TrimSpace(task.Result)
		if content == "" {
			content = "async task completed"
		}
		res.Content = mergeContentWithProgress(content, task.Progress)
		a.emit(AsyncToolUpdateMsg{SessionID: reg.SessionID, CallID: reg.CallID, Task: task, Result: &res})
		return false
	case string(tooling.AsyncTaskEventFailed):
		if task == nil {
			return false
		}
		errMsg := firstNonEmpty(strings.TrimSpace(ev.Error), strings.TrimSpace(task.Error), "async task failed")
		name := firstNonEmpty(strings.TrimSpace(task.ToolName), regToolName)
		if tooling.IsAgentCommandToolName(regToolName) && regToolName != "" {
			name = regToolName
		}
		res := tooltypes.Result{
			ToolCallID: strings.TrimSpace(reg.CallID),
			Name:       name,
			Metadata:   mergeMetadataWithProgress(task.Metadata, task.Progress),
			Content:    mergeContentWithProgress(errMsg, task.Progress),
			IsError:    true,
			Pending:    false,
		}
		a.emit(AsyncToolUpdateMsg{SessionID: reg.SessionID, CallID: reg.CallID, Task: task, Result: &res})
		return false
	case string(tooling.AsyncTaskEventDeleted):
		errMsg := firstNonEmpty(strings.TrimSpace(ev.Error), "async task deleted")
		a.emit(AsyncToolUpdateMsg{SessionID: reg.SessionID, CallID: reg.CallID, Error: errMsg})
		return false
	default:
		return true
	}
}

func (a *asyncTracker) poll(state *watchState) {

	for {
		if state == nil {
			return
		}
		reg := state.registration
		// Fetch task from the correct daemon
		daemonName := reg.Daemon
		if daemonName == "" {
			daemonName = "local"
		}
		task, err := tooling.FetchAsyncTaskFromDaemon(context.Background(), reg.TaskID, daemonName)
		if err != nil {
			errMsg := strings.TrimSpace(err.Error())
			if errMsg != "" && strings.Contains(strings.ToLower(errMsg), "not found") {
				a.emit(AsyncToolUpdateMsg{
					SessionID: reg.SessionID,
					CallID:    reg.CallID,
					Error:     errMsg,
				})
				return
			}
			time.Sleep(3 * time.Second)
			continue
		}
		if task == nil {
			time.Sleep(2 * time.Second)
			continue
		}
		enrichedMeta := mergeMetadataWithProgress(task.Metadata, task.Progress)
		if enrichedMeta != "" {
			task.Metadata = enrichedMeta
		}
		if len(task.Progress) > state.seen {
			progress := append([]tooling.AsyncTaskProgress(nil), task.Progress[state.seen:]...)
			state.seen = len(task.Progress)
			a.emit(AsyncToolUpdateMsg{
				SessionID: reg.SessionID,
				CallID:    reg.CallID,
				Task:      task,
				Progress:  progress,
			})
		}
		status := strings.ToLower(strings.TrimSpace(task.Status))
		if status == "complete" || status == "failed" {
			res := tooltypes.Result{
				ToolCallID: strings.TrimSpace(reg.CallID),
				Name:       firstNonEmpty(strings.TrimSpace(task.ToolName), strings.TrimSpace(reg.ToolName)),
				Metadata:   mergeMetadataWithProgress(task.Metadata, task.Progress),
				Pending:    false,
			}
			if status == "complete" {
				content := strings.TrimSpace(task.Result)
				if content == "" {
					content = "async task completed"
				}
				res.Content = mergeContentWithProgress(content, task.Progress)
			} else {
				errMsg := strings.TrimSpace(task.Error)
				if errMsg == "" {
					errMsg = "async task failed"
				}
				res.Content = mergeContentWithProgress(errMsg, task.Progress)
				res.IsError = true
			}
			a.emit(AsyncToolUpdateMsg{
				SessionID: reg.SessionID,
				CallID:    reg.CallID,
				Task:      task,
				Result:    &res,
			})
			return
		}
		time.Sleep(2 * time.Second)
	}
}

func (a *asyncTracker) emit(msg AsyncToolUpdateMsg) {
	if a == nil || a.updates == nil {
		return
	}
	select {
	case a.updates <- msg:
	default:
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func mergeContentWithProgress(content string, progress []tooling.AsyncTaskProgress) string {
	trimmedContent := strings.TrimSpace(content)
	if trimmedContent != "" {
		return trimmedContent
	}
	lines := make([]string, 0, len(progress))
	for _, entry := range progress {
		if text := strings.TrimSpace(entry.Text); text != "" {
			lines = append(lines, text)
		}
	}
	if len(lines) == 0 {
		return trimmedContent
	}
	return strings.Join(lines, "\n")
}

func mergeMetadataWithProgress(base string, progress []tooling.AsyncTaskProgress) string {
	trimmed := strings.TrimSpace(base)
	if len(progress) == 0 && trimmed == "" {
		return ""
	}
	var meta map[string]any
	if trimmed != "" {
		_ = json.Unmarshal([]byte(trimmed), &meta)
	}
	if meta == nil {
		meta = make(map[string]any)
	}
	if len(progress) > 0 {
		records := make([]map[string]any, 0, len(progress))
		for _, entry := range progress {
			record := map[string]any{}
			if !entry.Timestamp.IsZero() {
				record["timestamp"] = entry.Timestamp.Format(time.RFC3339)
			}
			if text := strings.TrimSpace(entry.Text); text != "" {
				record["text"] = text
			}
			if status := strings.TrimSpace(entry.Status); status != "" {
				record["status"] = status
			}
			if md := strings.TrimSpace(entry.Metadata); md != "" {
				record["metadata"] = md
			}
			if len(record) > 0 {
				records = append(records, record)
			}
		}
		if len(records) > 0 {
			meta["progress"] = records
		}
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return trimmed
	}
	return string(b)
}
