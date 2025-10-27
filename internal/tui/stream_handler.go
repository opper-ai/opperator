package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"

	llm "tui/llm"
	streaming "tui/streaming"
	tooltypes "tui/tools/types"
	toolstate "tui/toolstate"
)

const (
	cancelTimerDuration = 2 * time.Second
)

// ============================================================================
// Control Functions
// ============================================================================

func cancelTimerCmd(sessionID string) tea.Cmd {
	return tea.Tick(cancelTimerDuration, func(time.Time) tea.Msg { return cancelTimerExpiredMsg{SessionID: sessionID} })
}

func (m *Model) cancel() tea.Cmd {
	sessionID := m.sessionID
	state := m.streamState(sessionID)
	if state == nil {
		return nil
	}
	if state.Canceling {
		if state.Cancel != nil {
			state.Cancel()
		}
		return m.completeResponse(sessionID)
	}
	state.Canceling = true

	// Mark all pending tools as cancelled
	m.markPendingToolsAsCancelled(sessionID)

	m.refreshHelp()
	return cancelTimerCmd(sessionID)
}

func (m *Model) markPendingToolsAsCancelled(sessionID string) {
	mgr := m.streamManager()
	if mgr == nil {
		return
	}
	calls, order := mgr.PendingToolCalls(sessionID)
	if len(order) == 0 {
		return
	}
	for _, callID := range order {
		if _, exists := calls[callID]; exists {
			m.messages.SetToolLifecycle(callID, toolstate.LifecycleCancelled)
		}
	}
}

func (m *Model) cancelSessionFlow(sessionID string) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		id = m.sessionID
	}
	state := m.streamState(id)
	if state == nil {
		return
	}
	if state.Cancel != nil {
		state.Cancel()
	}
	m.completeResponse(id)
}

func (m *Model) completeResponse(sessionID string) tea.Cmd {
	if sessionID == "" {
		return nil
	}
	if sessionID == m.sessionID {
		m.messages.EndAssistant()
	}
	if state := m.streamState(sessionID); state != nil {
		state.Cancel = nil
		state.Channel = nil
		state.Canceling = false
		state.Waiting = false
	}

	// Check if there's a pending async resume before clearing the session.
	// This handles the race condition where an async task completes while
	// the LLM stream is still active.
	hasPendingResume := m.streamManager().HasPendingAsyncResume(sessionID)

	// Clear the pending resume flag BEFORE calling Clear() and BEFORE resuming
	// to prevent cascading resumes if another async completes during the resume.
	if hasPendingResume {
		m.streamManager().ClearPendingAsyncResume(sessionID)
	}

	m.streamManager().Clear(sessionID)
	if sessionID == m.sessionID {
		m.refreshHeaderMeta()
		m.refreshHelp()
		m.updateStatusForCurrentSession()
	}

	// If there was a pending async resume, trigger it now that the session is free
	if hasPendingResume {
		return m.autoResumeAfterAsyncResult(sessionID)
	}
	return nil
}

// ============================================================================
// Tool Call Tracking
// ============================================================================

func (m *Model) trackPendingToolCall(sessionID string, call tooltypes.Call) {
	m.streamManager().TrackToolCall(sessionID, call)
}

func (m *Model) setPendingToolReason(sessionID, callID, reason string) {
	m.streamManager().SetToolReason(sessionID, callID, reason)
}

func (m *Model) clearPendingToolCall(sessionID, callID string) {
	m.streamManager().ClearToolCall(sessionID, callID)
}

func (m *Model) hasPendingToolCalls(sessionID string) bool {
	calls, order := m.streamManager().PendingToolCalls(sessionID)
	if len(calls) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(order))
	for _, id := range order {
		seen[id] = struct{}{}
		call, ok := calls[id]
		if !ok {
			continue
		}
		if !call.Finished {
			return true
		}
	}
	for id, call := range calls {
		if _, ok := seen[id]; ok {
			continue
		}
		if !call.Finished {
			return true
		}
	}
	return false
}

func (m *Model) restorePendingToolCalls() tea.Cmd {
	calls, order := m.streamManager().PendingToolCalls(m.sessionID)
	if len(calls) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	if len(order) == 0 {
		for id := range calls {
			order = append(order, id)
		}
	}
	for _, id := range order {
		call, ok := calls[id]
		if !ok || call.Finished {
			continue
		}
		if cmd := m.messages.EnsureToolCall(call); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// ============================================================================
// Assistant State Management
// ============================================================================

func (m *Model) beginPendingAssistant(sessionID string) {
	m.streamManager().BeginAssistant(sessionID)
}

func (m *Model) recordPendingAssistantContent(sessionID, delta string) {
	m.streamManager().RecordAssistantContent(sessionID, delta)
}

func (m *Model) markPendingAssistantStreaming(sessionID string) {
	m.streamManager().MarkAssistantStreaming(sessionID)
}

func (m *Model) pendingAssistantState(sessionID string) *pendingAssistantState {
	pa := m.streamManager().PendingAssistant(sessionID)
	if pa == nil {
		return nil
	}
	return &pendingAssistantState{content: pa.Content, waiting: pa.Waiting}
}

func (m *Model) restorePendingAssistant() tea.Cmd {
	state := m.pendingAssistantState(m.sessionID)
	if state == nil {
		return nil
	}
	m.messages.AddAssistantStart(llm.ModelName())
	if state.content != "" {
		m.messages.SetActiveAssistantContent(state.content)
	}
	if state.waiting {
		return m.messages.StartLoading()
	}
	m.messages.StreamStarted(true)
	return nil
}

// ============================================================================
// Stream Message Handling
// ============================================================================

func (m *Model) handleStreamMsg(sessionID string, msg tea.Msg) tea.Cmd {
	if state := m.streamState(sessionID); state != nil {
		state.Waiting = false
	}
	switch v := msg.(type) {
	case llm.StreamStartedMsg:
		if v.Err != nil {
			errText := "[error: " + v.Err.Error() + "]"
			if sessionID == m.sessionID {
				m.messages.AppendAssistant(errText)
			} else {
				m.addAssistantContentHistory(sessionID, errText)
			}
			if cmd := m.completeResponse(sessionID); cmd != nil {
				return cmd
			}
			return nil
		}
		return m.nextStreamCmd()
	case llm.StreamDeltaMsg:
		if v.Text != "" {
			m.recordPendingAssistantContent(sessionID, v.Text)
			if sessionID == m.sessionID {
				m.messages.StreamStarted(true)
				m.messages.AppendAssistant(v.Text)
			}
		}
		return m.nextStreamCmd()
	case llm.StreamDoneMsg:
		if v.Err != nil {
			var errText string
			if errors.Is(v.Err, context.Canceled) {
				errText = "Request has been cancelled."
			} else {
				errText = "[error: " + v.Err.Error() + "]"
			}
			if sessionID == m.sessionID {
				m.messages.AppendAssistant(errText)
				m.messages.EndAssistant()
			} else {
				m.addAssistantContentHistory(sessionID, errText)
			}
		}
		var cmds []tea.Cmd
		if cmd := m.completeResponse(sessionID); cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, m.nextStreamCmd())
		return batchCmds(cmds)
	case llm.ToolUseStartMsg:
		m.markPendingAssistantStreaming(sessionID)
		m.trackPendingToolCall(sessionID, v.Call)
		var cmds []tea.Cmd
		if sessionID == m.sessionID {
			if cmd := m.messages.StartLoading(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			m.messages.StreamStarted(false)
			if cmd := m.messages.EnsureToolCall(v.Call); cmd != nil {
				cmds = append(cmds, cmd)
			}
			if cmd := m.refreshToolDetail(v.Call.ID); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		cmds = append(cmds, m.nextStreamCmd())
		return batchCmds(cmds)
	case llm.ToolUseFinishMsg:
		m.clearPendingToolCall(sessionID, v.Result.ToolCallID)
		if sessionID == m.sessionID {
			m.messages.FinishTool(v.Result.ToolCallID, v.Result)
			if !m.hasPendingToolCalls(sessionID) {
				m.messages.StopLoading()
			}
			if cmd := m.refreshToolDetail(v.Result.ToolCallID); cmd != nil {
				return tea.Batch(cmd, m.nextStreamCmd())
			}
		}
		return m.nextStreamCmd()
	case llm.ToolUseDeltaMsg:
		if sessionID == m.sessionID {
			m.messages.UpdateToolDelta(v.ID, v.Name, v.Delta)
			if cmd := m.refreshToolDetail(v.ID); cmd != nil {
				return tea.Batch(cmd, m.nextStreamCmd())
			}
		}
		return m.nextStreamCmd()
	case llm.SubAgentEventMsg:
		if sessionID == m.sessionID {
			id := v.ID
			ev := v.Ev
			m.messages.UpdateToolResultMeta(id, func(meta map[string]any) map[string]any {
				var transcript []any
				if existing, ok := meta["transcript"].([]any); ok {
					transcript = existing
				}
				if td := strings.TrimSpace(ev.TaskDefinition); td != "" {
					meta["task_definition"] = td
				}
				if name := strings.TrimSpace(ev.AgentName); name != "" {
					meta["agent_name"] = name
				}
				entry := map[string]any{
					"kind":                 ev.Kind,
					"status":               ev.Status,
					"content":              ev.Content,
					"tool_call_id":         ev.ToolCallID,
					"call_uid":             ev.CallUID,
					"tool_name":            ev.ToolName,
					"tool_input":           ev.ToolInput,
					"tool_result_content":  ev.ToolResultContent,
					"tool_result_metadata": ev.ToolResultMetadata,
				}
				transcript = append(transcript, entry)
				meta["transcript"] = transcript
				return meta
			})
			if cmd := m.refreshToolDetail(id); cmd != nil {
				return tea.Batch(cmd, m.nextStreamCmd())
			}
		}
		return m.nextStreamCmd()
	case llm.FollowupStartMsg:
		m.beginPendingAssistant(sessionID)
		if sessionID == m.sessionID {
			m.messages.AddAssistantStart(llm.ModelName())
			return tea.Batch(m.messages.StartLoading(), m.nextStreamCmd())
		}
		return m.nextStreamCmd()
	default:
		return nil
	}
}

// ============================================================================
// Stream Commands
// ============================================================================

func (m *Model) requestLLM(sessionID string) tea.Cmd {
	if m.llmEngine == nil {
		m.llmEngine = llm.NewEngine(m.permissions, m.secretPromptService(), m.workingDir, m.userWorkingDir, m.lspManager)
	}
	adapter := newSessionAdapter(m, sessionID)
	m.beginSpanTurnForSession(sessionID)
	cmd, cancel, ch := m.llmEngine.Request(adapter)
	state := &streaming.State{Cancel: cancel, Channel: ch}
	m.setStreamState(sessionID, state)
	return func() tea.Msg {
		msg := cmd()
		return sessionStreamMsg{SessionID: sessionID, Msg: msg}
	}
}

func (m *Model) nextStreamCmd() tea.Cmd {
	states := m.streamManager().States()
	if len(states) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for sessionID, state := range states {
		if state == nil || state.Channel == nil || state.Waiting {
			continue
		}
		state.Waiting = true
		cmds = append(cmds, m.recvAgent(sessionID, state.Channel))
	}
	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	default:
		return tea.Batch(cmds...)
	}
}

func (m *Model) recvAgent(sessionID string, ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return sessionStreamMsg{SessionID: sessionID, Msg: llm.StreamDoneMsg{}}
		}
		msg, ok := <-ch
		if !ok {
			return sessionStreamMsg{SessionID: sessionID, Msg: llm.StreamDoneMsg{}}
		}
		return sessionStreamMsg{SessionID: sessionID, Msg: msg}
	}
}
