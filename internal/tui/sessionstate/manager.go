package sessionstate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"tui/asyncutil"
	"tui/internal/conversation"
	"tui/internal/inputhistory"
	"tui/internal/message"
	tooling "tui/tools"
	tooltypes "tui/tools/types"
)

// Message captures a persisted conversation message for a session.


type Message struct {
	Role        string
	Content     string
	ToolCalls   []tooltypes.Call
	ToolResults []tooltypes.Result
	Turn        *TurnSummary
}

// TurnSummary captures completion metadata for an assistant turn.
type TurnSummary struct {
	AgentID       string
	AgentName     string
	AgentColor    string
	DurationMilli int64
}

type SpanState struct {
	rootID             string
	currentTurnID      string
	expectingTurnStart bool
}

const autoResumeHandledPrefix = "auto_resume_handled:"


// Manager orchestrates session history persistence and span bookkeeping.
type Manager struct {
	mu         sync.Mutex
	convStore  *conversation.Store
	msgStore   message.Service
	inputStore inputhistory.Service

	activeSessionID string
	history         []Message
	inputHistory    []string
	historyIdx      int

	spanStates         map[string]*SpanState
	handledToolResults map[string]struct{}
}

// NewManager constructs a session manager bound to persistence services.
func NewManager(convStore *conversation.Store, msgStore message.Service, inputStore inputhistory.Service) *Manager {
	return &Manager{
		convStore:          convStore,
		msgStore:           msgStore,
		inputStore:         inputStore,
		spanStates:         make(map[string]*SpanState),
		handledToolResults: make(map[string]struct{}),
	}
}

// ActiveSession returns the session currently cached in memory.
func (m *Manager) ActiveSession() string {
	return m.activeSessionID
}

// History returns a copy of the cached conversation history.
func (m *Manager) History() []Message {
	return append([]Message(nil), m.history...)
}

// InputHistory returns a copy of the cached input history.
func (m *Manager) InputHistory() []string {
	return append([]string(nil), m.inputHistory...)
}

// HistoryIndex reports the current traversal index through input history.
func (m *Manager) HistoryIndex() int {
	return m.historyIdx
}

func (m *Manager) SetHistoryIndex(idx int) {
	m.historyIdx = idx
}

func (m *Manager) AppendInput(ctx context.Context, sessionID, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(sessionID) == "" {
		return
	}
	if sessionID == m.activeSessionID {
		m.inputHistory = append(m.inputHistory, value)
		m.historyIdx = len(m.inputHistory)
	}
	if m.inputStore != nil && strings.TrimSpace(value) != "" {
		_ = m.inputStore.Add(ctx, sessionID, value)
	}
}

// LoadSession populates the in-memory caches for a session from the stores.
func (m *Manager) LoadSession(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(sessionID) == "" {
		m.activeSessionID = ""
		m.history = nil
		m.inputHistory = nil
		m.historyIdx = 0
		m.handledToolResults = make(map[string]struct{})
		return nil
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	msgs, err := m.msgStore.List(ctxWithTimeout, sessionID)
	if err != nil {
		return err
	}

	history := make([]Message, 0, len(msgs))
	var pendingSummary *TurnSummary
	handled := make(map[string]struct{})
	for _, msg := range msgs {
		role := strings.ToLower(string(msg.Role))
		if role == "system" {
			if summary := turnSummaryFromParts(msg.Parts); summary != nil {
				if len(history) > 0 {
					last := &history[len(history)-1]
					if strings.EqualFold(last.Role, "assistant") && last.Turn == nil {
						last.Turn = summary
						continue
					}
				}
				pendingSummary = summary
			}
			if callID := handledToolResultFromParts(msg.Parts); callID != "" {
				handled[callID] = struct{}{}
			}
			continue
		}

		hist := Message{Role: string(msg.Role), Content: msg.Content().String()}
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case message.ToolCall:
				hist.ToolCalls = append(hist.ToolCalls, tooltypes.Call{
					ID:       p.ID,
					Name:     p.Name,
					Input:    p.Input,
					Finished: p.Finished,
					Reason:   p.Reason,
				})
			case message.ToolResult:
				hist.ToolResults = append(hist.ToolResults, tooltypes.Result{
					ToolCallID: p.ToolCallID,
					Name:       p.Name,
					Content:    p.Content,
					Metadata:   p.Metadata,
					IsError:    p.IsError,
					Pending:    p.Pending,
				})
			case message.TurnSummary:
				if hist.Turn == nil {
					hist.Turn = &TurnSummary{
						AgentID:       strings.TrimSpace(p.AgentID),
						AgentName:     strings.TrimSpace(p.AgentName),
						AgentColor:    strings.TrimSpace(p.AgentColor),
						DurationMilli: p.DurationMilli,
					}
				}
			}
		}
		if hist.Turn == nil && pendingSummary != nil && strings.EqualFold(hist.Role, "assistant") {
			hist.Turn = pendingSummary
			pendingSummary = nil
		}
		history = append(history, hist)
	}

	m.activeSessionID = sessionID
	m.history = history
	m.handledToolResults = handled

	if m.inputStore != nil {
		if items, err := m.inputStore.List(ctx, sessionID); err == nil {
			m.inputHistory = items
			m.historyIdx = len(items)
			return nil
		}
	}

	m.inputHistory = nil
	m.historyIdx = 0
	return nil
}

// AppendUser persists a user message and appends it to the cached history.
func (m *Manager) AppendUser(ctx context.Context, sessionID, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.TrimSpace(sessionID) == "" {
		return
	}
	if sessionID == m.activeSessionID {
		m.history = append(m.history, Message{Role: "user", Content: text})
	}
	if m.msgStore != nil {
		_, _ = m.msgStore.Create(ctx, sessionID, message.CreateMessageParams{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: text}},
		})
	}
}

func (m *Manager) AppendAssistantContent(ctx context.Context, sessionID, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.TrimSpace(sessionID) == "" {
		return
	}
	if sessionID == m.activeSessionID {
		m.history = append(m.history, Message{Role: "assistant", Content: text})
	}
	if m.msgStore != nil {
		_, _ = m.msgStore.Create(ctx, sessionID, message.CreateMessageParams{
			Role:  message.Assistant,
			Parts: []message.ContentPart{message.TextContent{Text: text}},
		})
	}
}

func (m *Manager) AppendAssistantToolCalls(ctx context.Context, sessionID string, calls []tooltypes.Call, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	trimmedSession := strings.TrimSpace(sessionID)
	trimmedContent := strings.TrimSpace(content)

	if trimmedSession == "" || (len(calls) == 0 && trimmedContent == "") {
		return
	}

	storeToolCalls := len(calls) > 0
	if storeToolCalls {
		allExisting := true
		for _, call := range calls {
			id := strings.TrimSpace(call.ID)
			if id == "" {
				continue
			}
			if !m.toolCallExistsInStore(ctx, trimmedSession, id) {
				allExisting = false
				break
			}
		}
		storeToolCalls = !allExisting
	}

	if trimmedContent != "" {
		if trimmedSession == m.activeSessionID {
			m.history = append(m.history, Message{Role: "assistant", Content: content})
		}
		if m.msgStore != nil {
			_, _ = m.msgStore.Create(ctx, trimmedSession, message.CreateMessageParams{
				Role:  message.Assistant,
				Parts: []message.ContentPart{message.TextContent{Text: content}},
			})
		}
	}

	if !storeToolCalls {
		return
	}

	if trimmedSession == m.activeSessionID {
		m.history = append(m.history, Message{Role: string(message.ToolCallRole), ToolCalls: calls})
	}

	if m.msgStore == nil {
		return
	}

	var parts []message.ContentPart
	for _, call := range calls {
		isAsync := asyncutil.IsCall(call, content)
		parts = append(parts, message.ToolCall{
			ID:       call.ID,
			Name:     call.Name,
			Input:    call.Input,
			Type:     "function",
			Finished: call.Finished,
			Reason:   call.Reason,
			Async:    isAsync,
		})
	}
	if len(parts) == 0 {
		return
	}

	// Retry logic for database lock failures (SQLite BUSY errors)
	// Tool call persistence is critical, so we retry up to 3 times with exponential backoff
	maxRetries := 3
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 100ms, 200ms, 400ms
			backoff := time.Duration(100<<uint(attempt-1)) * time.Millisecond
			time.Sleep(backoff)
		}

		_, err := m.msgStore.Create(ctx, trimmedSession, message.CreateMessageParams{
			Role:  message.ToolCallRole,
			Parts: parts,
		})

		if err == nil {
			return
		}

		lastErr = err
		// Check if it's a database lock error - if so, retry
		errStr := strings.ToLower(err.Error())
		if !strings.Contains(errStr, "locked") && !strings.Contains(errStr, "busy") {
			// Not a lock error, don't retry
			break
		}
	}

	// If we get here, all retries failed - only log actual failures
	_ = lastErr // Suppress unused variable if we decide not to log
}

// AppendToolResults captures tool outputs in the conversation history.
func (m *Manager) AppendToolResults(ctx context.Context, sessionID string, results []tooltypes.Result) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(results) == 0 || strings.TrimSpace(sessionID) == "" {
		return
	}
	m.ensureToolCallsForResults(ctx, sessionID, results)

	filtered := make([]tooltypes.Result, 0, len(results))
	for _, result := range results {
		callID := strings.TrimSpace(result.ToolCallID)
		if callID != "" && isAsyncResultComplete(result) && m.hasCompletedToolResult(ctx, sessionID, callID) {
			continue
		}
		filtered = append(filtered, result)
	}
	if len(filtered) == 0 {
		return
	}

	if sessionID == m.activeSessionID {
		m.history = append(m.history, Message{Role: string(message.ToolCallResponseRole), ToolResults: filtered})
	}

	if m.msgStore == nil {
		return
	}

	var parts []message.ContentPart
	for _, result := range filtered {
		parts = append(parts, message.ToolResult{
			ToolCallID: result.ToolCallID,
			Name:       result.Name,
			Content:    result.Content,
			Metadata:   result.Metadata,
			IsError:    result.IsError,
			Pending:    result.Pending,
		})
	}
	_, _ = m.msgStore.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.ToolCallResponseRole,
		Parts: parts,
	})
}

func (m *Manager) ensureToolCallsForResults(ctx context.Context, sessionID string, results []tooltypes.Result) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || len(results) == 0 {
		return
	}

	for _, result := range results {
		id := strings.TrimSpace(result.ToolCallID)
		if id == "" {
			continue
		}
		call := tooltypes.Call{
			ID:       id,
			Name:     asyncToolNameFromMetadata(result.Metadata, result.Name),
			Input:    deriveAsyncCallInput(result),
			Finished: isAsyncResultComplete(result),
		}
		m.ensureToolCallLocked(ctx, sessionID, call)
	}
}

func asyncToolNameFromMetadata(metadata, fallback string) string {
	trimmed := strings.TrimSpace(metadata)
	if trimmed != "" {
		var wrapper struct {
			AsyncTask struct {
				Tool string `json:"tool"`
			} `json:"async_task"`
			Task struct {
				Tool string `json:"tool"`
			} `json:"task"`
		}
		if err := json.Unmarshal([]byte(trimmed), &wrapper); err == nil {
			if tool := strings.TrimSpace(wrapper.AsyncTask.Tool); tool != "" {
				return tool
			}
			if tool := strings.TrimSpace(wrapper.Task.Tool); tool != "" {
				return tool
			}
		}
	}
	if tool := strings.TrimSpace(fallback); tool != "" {
		return tool
	}
	return tooling.AsyncToolName
}

// ToolCallExists reports whether a tool call has already been persisted.
func (m *Manager) ToolCallExists(ctx context.Context, sessionID, callID string) bool {
	return m.toolCallExistsInStore(ctx, strings.TrimSpace(sessionID), strings.TrimSpace(callID))
}

func (m *Manager) hasCompletedToolResult(ctx context.Context, sessionID, callID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	callID = strings.TrimSpace(callID)
	if sessionID == "" || callID == "" {
		return false
	}

	if sessionID == m.activeSessionID {
		for _, msg := range m.history {
			for _, result := range msg.ToolResults {
				if strings.EqualFold(strings.TrimSpace(result.ToolCallID), callID) && isAsyncResultComplete(result) {
					return true
				}
			}
		}
	}

	if m.msgStore == nil {
		return false
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	msgs, err := m.msgStore.List(ctxWithTimeout, sessionID)
	if err != nil {
		return false
	}
	for _, msg := range msgs {
		role := strings.ToLower(string(msg.Role))
		if role != strings.ToLower(string(message.ToolCallResponseRole)) && role != strings.ToLower(string(message.Tool)) {
			continue
		}
		for _, part := range msg.ToolResults() {
			if !strings.EqualFold(strings.TrimSpace(part.ToolCallID), callID) {
				continue
			}
			if isAsyncResultComplete(tooltypes.Result{
				ToolCallID: part.ToolCallID,
				Name:       part.Name,
				Content:    part.Content,
				Metadata:   part.Metadata,
				IsError:    part.IsError,
				Pending:    part.Pending,
			}) {
				return true
			}
		}
	}
	return false
}

func deriveAsyncCallInput(result tooltypes.Result) string {
	trimmed := strings.TrimSpace(result.Content)
	if trimmed == "" {
		trimmed = "async task pending"
	}
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "async task") {
		trimmed = fmt.Sprintf("async task result: %s", trimmed)
	}
	return trimmed
}

func isAsyncResultComplete(result tooltypes.Result) bool {
	// Errors are always considered complete
	if result.IsError {
		return true
	}
	return !result.Pending
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// EnsureToolCall guarantees a tool call is persisted for the session.
func (m *Manager) EnsureToolCall(ctx context.Context, sessionID string, call tooltypes.Call) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ensureToolCallLocked(ctx, sessionID, call)
}

func (m *Manager) ensureToolCallLocked(ctx context.Context, sessionID string, call tooltypes.Call) {
	sessionID = strings.TrimSpace(sessionID)
	id := strings.TrimSpace(call.ID)
	if sessionID == "" || id == "" {
		return
	}

	if m.toolCallExistsInHistory(sessionID, id) {
		return
	}

	if !m.toolCallExistsInStore(ctx, sessionID, id) {
		canonical := tooltypes.Call{
			ID:       id,
			Name:     firstNonEmpty(strings.TrimSpace(call.Name), tooling.AsyncToolName),
			Input:    strings.TrimSpace(call.Input),
			Finished: call.Finished,
			Reason:   strings.TrimSpace(call.Reason),
		}
		if canonical.Name == "" {
			canonical.Name = tooling.AsyncToolName
		}
		if sessionID == m.activeSessionID {
			m.history = append(m.history, Message{Role: string(message.ToolCallRole), ToolCalls: []tooltypes.Call{canonical}})
		}
		if m.msgStore != nil {
			parts := []message.ContentPart{message.ToolCall{
				ID:       canonical.ID,
				Name:     canonical.Name,
				Input:    canonical.Input,
				Type:     "function",
				Finished: canonical.Finished,
				Reason:   canonical.Reason,
				Async:    asyncutil.IsCall(canonical),
			}}
			_, _ = m.msgStore.Create(ctx, sessionID, message.CreateMessageParams{
				Role:  message.ToolCallRole,
				Parts: parts,
			})
		}
	}
}

func (m *Manager) toolCallExistsInHistory(sessionID, id string) bool {
	if sessionID != m.activeSessionID {
		return false
	}
	for _, msg := range m.history {
		for _, call := range msg.ToolCalls {
			if strings.TrimSpace(call.ID) == id {
				return true
			}
		}
	}
	return false
}

func (m *Manager) toolCallExistsInStore(ctx context.Context, sessionID, id string) bool {
	if m.msgStore == nil {
		return false
	}

	// Add timeout to prevent hanging queries
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	msgs, err := m.msgStore.List(ctxWithTimeout, sessionID)
	if err != nil {
		return false
	}
	for _, msg := range msgs {
		for _, call := range msg.ToolCalls() {
			if strings.TrimSpace(call.ID) == id {
				return true
			}
		}
	}
	return false
}

// ToolResultHandled reports whether the given tool call has already triggered
func (m *Manager) ToolResultHandled(ctx context.Context, sessionID, callID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	callID = strings.TrimSpace(callID)
	if sessionID == "" || callID == "" {
		return false
	}
	if sessionID == m.activeSessionID {
		_, ok := m.handledToolResults[callID]
		return ok
	}
	if m.msgStore == nil {
		return false
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	msgs, err := m.msgStore.List(ctxWithTimeout, sessionID)
	if err != nil {
		return false
	}
	for _, msg := range msgs {
		if strings.ToLower(string(msg.Role)) != "system" {
			continue
		}
		if handledToolResultFromParts(msg.Parts) == callID {
			return true
		}
	}
	return false
}

// MarkToolResultHandled persists that the given tool call has triggered an
// automatic follow-up turn so it will not fire again on restore.
func (m *Manager) MarkToolResultHandled(ctx context.Context, sessionID, callID string) {
	sessionID = strings.TrimSpace(sessionID)
	callID = strings.TrimSpace(callID)
	if sessionID == "" || callID == "" {
		return
	}
	if m.ToolResultHandled(ctx, sessionID, callID) {
		return
	}
	if sessionID == m.activeSessionID {
		if m.handledToolResults == nil {
			m.handledToolResults = make(map[string]struct{})
		}
		m.handledToolResults[callID] = struct{}{}
	}
	if m.msgStore == nil {
		return
	}
	text := handledToolResultSystemText(callID)
	_, _ = m.msgStore.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.System,
		Parts: []message.ContentPart{message.TextContent{Text: text}},
	})
}

// AppendTurnSummary persists turn completion metadata for the latest assistant message.
func (m *Manager) AppendTurnSummary(ctx context.Context, sessionID string, summary TurnSummary) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(sessionID) == "" {
		return
	}
	trimmedID := strings.TrimSpace(summary.AgentID)
	trimmedName := strings.TrimSpace(summary.AgentName)
	trimmedColor := strings.TrimSpace(summary.AgentColor)
	if trimmedID == "" && trimmedName == "" && summary.DurationMilli <= 0 {
		return
	}

	if sessionID == m.activeSessionID {
		for i := len(m.history) - 1; i >= 0; i-- {
			if !strings.EqualFold(m.history[i].Role, "assistant") {
				continue
			}
			cp := summary
			cp.AgentID = trimmedID
			cp.AgentName = trimmedName
			cp.AgentColor = trimmedColor
			m.history[i].Turn = &cp
			break
		}
	}

	if m.msgStore == nil {
		return
	}

	part := message.TurnSummary{
		AgentID:       trimmedID,
		AgentName:     trimmedName,
		AgentColor:    trimmedColor,
		DurationMilli: summary.DurationMilli,
	}
	_, _ = m.msgStore.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.System,
		Parts: []message.ContentPart{part},
	})
}

func (m *Manager) MaybeUpdateTitle(ctx context.Context, text string) {
	if m.convStore == nil || len(m.history) != 1 || strings.TrimSpace(m.activeSessionID) == "" {
		return
	}
	title := text
	if len(title) > 50 {
		title = title[:47] + "..."
	}
	m.convStore.UpdateTitle(ctx, m.activeSessionID, title)
}

func (m *Manager) ensureSpanState(sessionID string) *SpanState {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if m.spanStates == nil {
		m.spanStates = make(map[string]*SpanState)
	}
	state, ok := m.spanStates[sessionID]
	if !ok || state == nil {
		state = &SpanState{}
		m.spanStates[sessionID] = state
	}
	return state
}

// BeginSpan marks a new assistant turn for a session.
func (m *Manager) BeginSpan(sessionID string) {
	state := m.ensureSpanState(sessionID)
	if state == nil {
		return
	}
	state.expectingTurnStart = true
	state.currentTurnID = ""
}

// RecordSpanID records a span identifier for tracing.
func (m *Manager) RecordSpanID(sessionID, spanID string) {
	spanID = strings.TrimSpace(spanID)
	if spanID == "" {
		return
	}
	state := m.ensureSpanState(sessionID)
	if state == nil {
		return
	}
	if state.expectingTurnStart || state.currentTurnID == "" {
		state.currentTurnID = spanID
		state.expectingTurnStart = false
	}
	if state.rootID == "" {
		state.rootID = spanID
	}
}

func (m *Manager) ParentSpanID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || m.spanStates == nil {
		return ""
	}
	state := m.spanStates[sessionID]
	if state == nil {
		return ""
	}
	if state.expectingTurnStart {
		return strings.TrimSpace(state.rootID)
	}
	if state.currentTurnID != "" {
		return strings.TrimSpace(state.currentTurnID)
	}
	return strings.TrimSpace(state.rootID)
}

func (m *Manager) ClearSpan(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || m.spanStates == nil {
		return
	}
	delete(m.spanStates, sessionID)
}

func (m *Manager) ConversationHistory(ctx context.Context, sessionID string) []Message {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if sessionID == m.activeSessionID {
		return m.History()
	}
	if m.msgStore == nil {
		return nil
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	msgs, err := m.msgStore.List(ctxWithTimeout, sessionID)
	if err != nil {
		return nil
	}
	history := make([]Message, 0, len(msgs))
	var pendingSummary *TurnSummary
	for _, msg := range msgs {
		role := strings.ToLower(string(msg.Role))
		if role == "system" {
			if summary := turnSummaryFromParts(msg.Parts); summary != nil {
				if len(history) > 0 {
					last := &history[len(history)-1]
					if strings.EqualFold(last.Role, "assistant") && last.Turn == nil {
						last.Turn = summary
						continue
					}
				}
				pendingSummary = summary
			}
			continue
		}

		hist := Message{Role: string(msg.Role), Content: msg.Content().String()}
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case message.ToolCall:
				hist.ToolCalls = append(hist.ToolCalls, tooltypes.Call{
					ID:       p.ID,
					Name:     p.Name,
					Input:    p.Input,
					Finished: p.Finished,
					Reason:   p.Reason,
				})
			case message.ToolResult:
				hist.ToolResults = append(hist.ToolResults, tooltypes.Result{
					ToolCallID: p.ToolCallID,
					Name:       p.Name,
					Content:    p.Content,
					Metadata:   p.Metadata,
					IsError:    p.IsError,
					Pending:    p.Pending,
				})
			case message.TurnSummary:
				if hist.Turn == nil {
					hist.Turn = &TurnSummary{
						AgentID:       strings.TrimSpace(p.AgentID),
						AgentName:     strings.TrimSpace(p.AgentName),
						AgentColor:    strings.TrimSpace(p.AgentColor),
						DurationMilli: p.DurationMilli,
					}
				}
			}
		}
		if hist.Turn == nil && pendingSummary != nil && strings.EqualFold(hist.Role, "assistant") {
			hist.Turn = pendingSummary
			pendingSummary = nil
		}
		history = append(history, hist)
	}
	return history
}

// LastAssistantContent walks the history to find the most recent assistant text.
func (m *Manager) LastAssistantContent(ctx context.Context, sessionID string) string {
	history := m.ConversationHistory(ctx, sessionID)
	for i := len(history) - 1; i >= 0; i-- {
		h := history[i]
		if strings.ToLower(h.Role) != "assistant" {
			continue
		}
		if content := strings.TrimSpace(h.Content); content != "" {
			return content
		}
	}
	return ""
}

func turnSummaryFromParts(parts []message.ContentPart) *TurnSummary {
	for _, part := range parts {
		if p, ok := part.(message.TurnSummary); ok {
			if strings.TrimSpace(p.AgentID) == "" && p.DurationMilli <= 0 && strings.TrimSpace(p.AgentName) == "" && strings.TrimSpace(p.AgentColor) == "" {
				continue
			}
			return &TurnSummary{
				AgentID:       strings.TrimSpace(p.AgentID),
				AgentName:     strings.TrimSpace(p.AgentName),
				AgentColor:    strings.TrimSpace(p.AgentColor),
				DurationMilli: p.DurationMilli,
			}
		}
	}
	return nil
}

func handledToolResultFromParts(parts []message.ContentPart) string {
	for _, part := range parts {
		if text, ok := part.(message.TextContent); ok {
			trimmed := strings.TrimSpace(text.Text)
			if strings.HasPrefix(trimmed, autoResumeHandledPrefix) {
				callID := strings.TrimSpace(strings.TrimPrefix(trimmed, autoResumeHandledPrefix))
				if callID != "" {
					return callID
				}
			}
		}
	}
	return ""
}

func handledToolResultSystemText(callID string) string {
	return autoResumeHandledPrefix + strings.TrimSpace(callID)
}
