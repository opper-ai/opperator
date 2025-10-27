package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
)

const (
	MaxAsyncProgressLines     = 10
	asyncProgressCleanupDelay = 2 * time.Minute
)

// labelPriority determines which label source takes precedence.
type labelPriority uint8

const (
	labelPriorityNone     labelPriority = iota // No label set
	labelPriorityFallback                      // From tool/command name
	labelPriorityMetadata                      // From metadata field
	labelPriorityExternal                      // From external snapshot
)

// asyncStateManager manages the state for all async tool calls.
type asyncStateManager struct {
	mu     sync.RWMutex
	states map[string]*asyncState
}

type asyncState struct {
	manager *asyncStateManager
	callID  string

	mu            sync.Mutex
	taskID        string
	label         string
	labelPriority labelPriority
	lines         []string
	watching      bool
	done          bool
	cancel        context.CancelFunc
}

var globalAsyncManager = newAsyncStateManager()

var asyncStateLog = func(format string, args ...any) {}

func SetAsyncStateLogger(fn func(string, ...any)) {
	if fn == nil {
		asyncStateLog = func(string, ...any) {}
		return
	}
	asyncStateLog = fn
}

func newAsyncStateManager() *asyncStateManager {
	return &asyncStateManager{
		states: make(map[string]*asyncState),
	}
}

// This is the ONLY method that should be called from rendering code.
func (m *asyncStateManager) GetViewModel(call tooltypes.Call, result tooltypes.Result) AsyncViewModel {
	state := m.ensureState(call, result)
	if state == nil {
		return AsyncViewModel{
			Label:  "Async",
			Status: "Pending",
		}
	}

	label, lines := state.snapshot()
	label = preferDefinitionLabel(label, call, result)
	status := determineStatus(call, result)
	showSpinner := !call.Finished && !result.IsError

	asyncStateLog("[async_state] GetViewModel | callID=%s label=%q status=%s async=%v lines=%d", strings.TrimSpace(call.ID), label, status, call.Finished == false, len(lines))
	return AsyncViewModel{
		Label:       label,
		Status:      status,
		Lines:       lines,
		ShowSpinner: showSpinner,
	}
}

// UpdateSnapshot allows external systems to push updates.
func (m *asyncStateManager) UpdateSnapshot(callID, label string, lines []string, finished bool) {
	state := m.getOrCreateState(callID)
	if state == nil {
		return
	}
	state.applySnapshot(label, lines, finished)
}

// ensureState gets or creates state and processes call/result data.
func (m *asyncStateManager) ensureState(call tooltypes.Call, result tooltypes.Result) *asyncState {
	callID := getCallID(call, result)
	if callID == "" {
		return nil
	}

	state := m.getOrCreateState(callID)
	if state == nil {
		return nil
	}

	state.processCallResult(call, result)
	asyncStateLog("[async_state] ensureState | callID=%s callName=%s resultName=%s", callID, strings.TrimSpace(call.Name), strings.TrimSpace(result.Name))

	return state
}

func (m *asyncStateManager) getOrCreateState(callID string) *asyncState {
	id := strings.TrimSpace(callID)
	if id == "" {
		return nil
	}

	// Fast path: read lock
	m.mu.RLock()
	state := m.states[id]
	m.mu.RUnlock()
	if state != nil {
		return state
	}

	// Slow path: write lock
	m.mu.Lock()
	defer m.mu.Unlock()
	if state = m.states[id]; state == nil {
		state = &asyncState{
			manager: m,
			callID:  id,
		}
		m.states[id] = state
	}
	return state
}

func (m *asyncStateManager) scheduleCleanup(callID string) {
	if callID == "" {
		return
	}
	go func() {
		timer := time.NewTimer(asyncProgressCleanupDelay)
		defer timer.Stop()
		<-timer.C
		m.mu.Lock()
		if state, ok := m.states[callID]; ok && state.isDisposable() {
			delete(m.states, callID)
		}
		m.mu.Unlock()
	}()
}

// asyncState methods

func (s *asyncState) processCallResult(call tooltypes.Call, result tooltypes.Result) {
	parser := newMetadataParser()

	// Extract task ID
	taskID := firstNonEmptyString(
		extractAsyncTaskID(parser, parser.parse(result.Metadata)),
		extractAsyncTaskID(parser, parser.parse(call.Input)),
		extractAsyncTaskID(parser, parser.parse(call.Reason)),
	)
	if taskID != "" {
		s.setTaskID(taskID)
	}

	// Extract label
	label := extractLabel(parser, call, result)
	priority := determineLabelPriority(parser, result.Metadata, call.Input)
	if label != "" {
		s.updateLabel(label, priority)
	}
	asyncStateLog("[async_state] processCallResult | callID=%s taskID=%s label=%q priority=%d async=%v", s.callID, taskID, label, priority, call.Finished == false)

	// Extract initial progress lines
	lines := extractProgressLines(parser, parser.parse(result.Metadata))
	if len(lines) > 0 {
		s.ensureInitial(lines)
	}

	if call.Finished || result.IsError {
		s.markDone()
	}

	// Start watcher if needed
	s.maybeStartWatcher()
}

func (s *asyncState) setTaskID(taskID string) {
	trimmed := strings.TrimSpace(taskID)
	if trimmed == "" {
		return
	}
	s.mu.Lock()
	if s.taskID == "" {
		s.taskID = trimmed
	}
	s.mu.Unlock()
}

func (s *asyncState) updateLabel(candidate string, priority labelPriority) {
	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		return
	}
	s.mu.Lock()
	prev := s.label
	prevPriority := s.labelPriority
	shouldUpdate := false
	if priority > s.labelPriority {
		shouldUpdate = true
	} else if priority == s.labelPriority {
		if strings.TrimSpace(s.label) == "" {
			shouldUpdate = true
		} else if priority == labelPriorityExternal && !strings.EqualFold(s.label, trimmed) {
			shouldUpdate = true
		}
	}
	if shouldUpdate {
		s.label = trimmed
		s.labelPriority = priority
	}
	updated := s.label
	updatedPriority := s.labelPriority
	s.mu.Unlock()
	if updated != prev || updatedPriority != prevPriority {
		asyncStateLog("[async_state] updateLabel | callID=%s prev=%q new=%q prevPriority=%d newPriority=%d", s.callID, prev, updated, prevPriority, updatedPriority)
	}
}

func (s *asyncState) ensureInitial(lines []string) {
	normalized := normalizeLines(lines)
	if len(normalized) == 0 {
		return
	}
	s.mu.Lock()
	if len(s.lines) == 0 {
		s.lines = normalized
		if len(normalized) > 0 {
			asyncStateLog("[async_state] ensureInitial | callID=%s lines=%d", s.callID, len(normalized))
		}
	}
	s.mu.Unlock()
}

func (s *asyncState) applySnapshot(label string, lines []string, finished bool) {
	if trimmed := strings.TrimSpace(label); trimmed != "" {
		s.updateLabel(trimmed, labelPriorityExternal)
	}
	if normalized := normalizeLines(lines); len(normalized) > 0 {
		s.mu.Lock()
		s.lines = normalized
		s.mu.Unlock()
	}
	if finished {
		s.markDone()
	}
}

func (s *asyncState) maybeStartWatcher() {
	s.mu.Lock()
	if s.done || s.taskID == "" || s.watching {
		s.mu.Unlock()
		return
	}
	taskID := s.taskID
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.watching = true
	s.mu.Unlock()

	go s.runWatcher(ctx, taskID)
}

func (s *asyncState) runWatcher(ctx context.Context, taskID string) {
	events, stop, err := WatchAsyncTask(ctx, taskID)
	if err != nil {
		s.appendLine(fmt.Sprintf("error opening async stream: %v", err))
		s.finishWatcher()
		return
	}
	defer stop()

	for ev := range events {
		if !s.processEvent(ev) {
			break
		}
	}
	s.finishWatcher()
}

func (s *asyncState) processEvent(ev AsyncTaskEvent) bool {
	eventType := strings.ToLower(strings.TrimSpace(string(ev.Type)))

	switch eventType {
	case "", string(AsyncTaskEventSnapshot):
		if ev.Task != nil {
			s.updateFromTask(ev.Task)
			if len(ev.Task.Progress) > 0 {
				s.setSnapshot(ev.Task.Progress)
			}
		}
		return true

	case string(AsyncTaskEventProgress):
		if ev.Task != nil {
			s.updateFromTask(ev.Task)
		}
		if ev.Progress != nil {
			s.addProgressEntry(*ev.Progress)
		}
		return true

	case string(AsyncTaskEventCompleted):
		if ev.Task != nil {
			s.updateFromTask(ev.Task)
			if res := strings.TrimSpace(ev.Task.Result); res != "" {
				s.appendLine(res)
			}
		}
		s.appendLine("completed")
		s.markDone()
		return false

	case string(AsyncTaskEventFailed):
		if ev.Task != nil {
			s.updateFromTask(ev.Task)
		}
		msg := firstNonEmptyString(ev.Error)
		if ev.Task != nil {
			msg = firstNonEmptyString(ev.Error, ev.Task.Error, msg)
		}
		if msg == "" {
			msg = "failed"
		}
		s.appendLine(fmt.Sprintf("failed: %s", msg))
		s.markDone()
		return false

	case string(AsyncTaskEventDeleted):
		msg := strings.TrimSpace(ev.Error)
		if msg == "" {
			msg = "task deleted"
		}
		s.appendLine(msg)
		s.markDone()
		return false

	default:
		if ev.Progress != nil {
			s.addProgressEntry(*ev.Progress)
		}
		return true
	}
}

func (s *asyncState) updateFromTask(task *AsyncTask) {
	if task == nil {
		return
	}

	if name := firstNonEmptyString(task.CommandName, task.ToolName); name != "" {
		s.updateLabel(toolregistry.PrettifyName(name), labelPriorityFallback)
	}

	// Extract label from metadata if present
	if trimmed := strings.TrimSpace(task.Metadata); trimmed != "" {
		parser := newMetadataParser()
		if label := extractProgressLabel(parser, parser.parse(trimmed)); label != "" {
			s.updateLabel(label, labelPriorityMetadata)
		}
	}
}

func (s *asyncState) setSnapshot(entries []AsyncTaskProgress) {
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		if line := formatProgressEntry(entry); line != "" {
			lines = append(lines, line)
		}
	}
	normalized := normalizeLines(lines)
	if len(normalized) == 0 {
		return
	}
	s.mu.Lock()
	s.lines = normalized
	count := len(s.lines)
	s.mu.Unlock()
	asyncStateLog("[async_state] setSnapshot | callID=%s lines=%d", s.callID, count)
}

func (s *asyncState) addProgressEntry(entry AsyncTaskProgress) {
	if line := formatProgressEntry(entry); line != "" {
		asyncStateLog("[async_state] addProgressEntry | callID=%s line=%q status=%s", s.callID, line, strings.TrimSpace(entry.Status))
		s.appendLine(line)
	}
}

func (s *asyncState) appendLine(line string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}
	s.mu.Lock()
	// Deduplicate consecutive lines
	if len(s.lines) > 0 && s.lines[len(s.lines)-1] == trimmed {
		s.mu.Unlock()
		asyncStateLog("[async_state] appendLine dedupe | callID=%s line=%q", s.callID, trimmed)
		return
	}
	s.lines = append(s.lines, trimmed)
	// Keep only last N lines
	if len(s.lines) > MaxAsyncProgressLines {
		start := len(s.lines) - MaxAsyncProgressLines
		trimmedLines := make([]string, MaxAsyncProgressLines)
		copy(trimmedLines, s.lines[start:])
		s.lines = trimmedLines
	}
	count := len(s.lines)
	s.mu.Unlock()
	asyncStateLog("[async_state] appendLine | callID=%s line=%q totalLines=%d", s.callID, trimmed, count)
}

func (s *asyncState) markDone() {
	s.mu.Lock()
	alreadyDone := s.done
	s.done = true
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if !alreadyDone && s.manager != nil {
		s.manager.scheduleCleanup(s.callID)
	}
}

func (s *asyncState) finishWatcher() {
	s.mu.Lock()
	s.watching = false
	s.cancel = nil
	done := s.done
	s.mu.Unlock()

	if done && s.manager != nil {
		s.manager.scheduleCleanup(s.callID)
	}
}

func (s *asyncState) isDisposable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done && !s.watching
}

func (s *asyncState) snapshot() (string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	label := s.label
	if label == "" {
		label = "Async"
	}

	var lines []string
	if len(s.lines) > 0 {
		lines = make([]string, len(s.lines))
		copy(lines, s.lines)
	}

	return label, lines
}


func getCallID(call tooltypes.Call, result tooltypes.Result) string {
	if trimmed := strings.TrimSpace(call.ID); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(result.ToolCallID)
}

func extractLabel(parser *metadataParser, call tooltypes.Call, result tooltypes.Result) string {
	// Try metadata first
	if label := extractProgressLabel(parser, parser.parse(result.Metadata)); label != "" {
		return label
	}
	if label := extractProgressLabel(parser, parser.parse(call.Input)); label != "" {
		return label
	}

	// Fall back to tool/command names
	if trimmed := strings.TrimSpace(result.Name); trimmed != "" {
		return toolregistry.PrettifyName(trimmed)
	}
	if trimmed := strings.TrimSpace(call.Name); trimmed != "" {
		return toolregistry.PrettifyName(trimmed)
	}

	return ""
}

func determineLabelPriority(parser *metadataParser, metadata, input string) labelPriority {
	if extractProgressLabel(parser, parser.parse(metadata)) != "" {
		return labelPriorityMetadata
	}
	if extractProgressLabel(parser, parser.parse(input)) != "" {
		return labelPriorityMetadata
	}
	return labelPriorityFallback
}

func determineStatus(call tooltypes.Call, result tooltypes.Result) string {
	if result.IsError {
		return "Failed"
	}
	if call.Finished {
		return "Completed"
	}
	return "Running"
}

func preferDefinitionLabel(current string, call tooltypes.Call, result tooltypes.Result) string {
	trimmed := strings.TrimSpace(current)
	desired := firstNonEmptyString(
		definitionLabel(result.Name),
		definitionLabel(call.Name),
	)
	if desired == "" {
		if trimmed == "" {
			return "Async"
		}
		return trimmed
	}

	if trimmed == "" || isFallbackAsyncLabel(trimmed, call, result) {
		return desired
	}

	return trimmed
}

func formatProgressEntry(entry AsyncTaskProgress) string {
	text := strings.TrimSpace(entry.Text)
	status := strings.TrimSpace(entry.Status)

	switch {
	case text != "" && status != "":
		return fmt.Sprintf("%s — %s", status, text)
	case text != "":
		return text
	case status != "":
		return status
	default:
		if !entry.Timestamp.IsZero() {
			return entry.Timestamp.Format(time.RFC3339)
		}
		return ""
	}
}
