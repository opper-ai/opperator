package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"

	"tui/coreagent"
	"tui/internal/protocol"
	llm "tui/llm"
	sessionstate "tui/sessionstate"
	streaming "tui/streaming"
	tooling "tui/tools"
	tooltypes "tui/tools/types"
)

// ============================================================================
// Session Adapter
// ============================================================================

func newSessionAdapter(m *Model, sessionID string) *sessionstate.Adapter {
	options, listErr := m.agents.collectSessionAgentOptions()
	activeName := m.currentActiveAgentName()
	activePrompt := m.currentActiveAgentPrompt()
	activeCommands := m.currentActiveAgentCommands()
	corePrompt := m.currentCoreAgentPrompt()
	activeColor := m.currentActiveAgentColor()
	coreID := m.currentCoreAgentID()
	coreName := m.currentCoreAgentName()
	coreColor := m.currentCoreAgentColor()

	// Build focused agent info
	focusedAgentInfo := sessionstate.FocusedAgentInfo{}
	if m.agents != nil {
		focusedAgentInfo.Name = m.agents.focusedAgent()
	}
	if m.sidebar != nil && coreID == coreagent.IDBuilder {
		focusedAgentInfo.Description = m.sidebar.FocusedAgentDescription()
		sidebarTodos := m.sidebar.FocusedAgentTodos()
		focusedAgentInfo.Todos = make([]sessionstate.TodoInfo, len(sidebarTodos))
		for i, todo := range sidebarTodos {
			focusedAgentInfo.Todos[i] = sessionstate.TodoInfo{
				Text:      todo.Text,
				Completed: todo.Completed,
			}
		}
	}

	return sessionstate.NewAdapter(
		m.sessionManager(),
		sessionID,
		sessionstate.AdapterOptions{
			AgentName:        activeName,
			AgentColor:       activeColor,
			AgentPrompt:      activePrompt,
			AgentCommands:    append([]protocol.CommandDescriptor(nil), activeCommands...),
			CorePrompt:       corePrompt,
			CoreAgentID:      coreID,
			CoreAgentName:    coreName,
			CoreAgentColor:   coreColor,
			BaseSpecs:        m.baseToolSpecsForSession(),
			AgentOptions:     options,
			AgentListErr:     listErr,
			FocusedAgentInfo: focusedAgentInfo,
			ExtraToolSpecs: func() []tooling.Spec {
				return m.extraToolSpecsForSession()
			},
		},
	)
}

// ============================================================================
// Stream State
// ============================================================================

func (m *Model) streamState(sessionID string) *streaming.State {
	return m.streamManager().State(sessionID)
}

func (m *Model) setStreamState(sessionID string, state *streaming.State) {
	m.streamManager().SetState(sessionID, state)
}

func (m *Model) isSessionBusy(sessionID string) bool {
	return m.streamManager().IsBusy(sessionID)
}

func (m *Model) isCancelingSession(sessionID string) bool {
	return m.streamManager().IsCanceling(sessionID)
}

func (m *Model) updateStatusForCurrentSession() {
	if m.stats == nil {
		return
	}
	if m.isSessionBusy(m.sessionID) {
		m.stats.SetStatus("Typing…")
	} else {
		m.stats.SetStatus("Ready")
	}
}

// ============================================================================
// History Management
// ============================================================================

func (m *Model) addUserHistory(text string) {
	m.sessionManager().AppendUser(context.Background(), m.sessionID, text)
	m.maybeUpdateConversationTitle(text)
}

func (m *Model) addAssistantContentHistory(sessionID, text string) {
	m.sessionManager().AppendAssistantContent(context.Background(), sessionID, text)
}

func (m *Model) addAssistantToolCallsHistory(sessionID string, calls []tooltypes.Call, content string) {
	m.sessionManager().AppendAssistantToolCalls(context.Background(), sessionID, calls, content)
}

func (m *Model) addToolResultsHistory(sessionID string, results []tooltypes.Result) {
	m.sessionManager().AppendToolResults(context.Background(), sessionID, results)
}

// ============================================================================
// Recording for Session
// ============================================================================

func (m *Model) recordAssistantContentForSession(sessionID, text string) {
	m.sessionManager().AppendAssistantContent(context.Background(), sessionID, text)
}

func (m *Model) recordAssistantToolCallsForSession(sessionID string, calls []tooltypes.Call, content string) {
	m.sessionManager().AppendAssistantToolCalls(context.Background(), sessionID, calls, content)
}

func (m *Model) recordToolResultsForSession(sessionID string, results []tooltypes.Result) tea.Cmd {
	m.sessionManager().AppendToolResults(context.Background(), sessionID, results)
	if sessionID == m.sessionID && m.messages != nil {
		var cmds []tea.Cmd
		for _, result := range results {
			m.messages.SetPendingToolResult(result.ToolCallID, result)
			if cmd := m.refreshToolDetail(result.ToolCallID); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return batchCmds(cmds)
	}
	return nil
}

// ============================================================================
// Auto Resume
// ============================================================================

func (m *Model) autoResumeAfterAsyncResult(sessionID string) tea.Cmd {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		trimmed = strings.TrimSpace(m.sessionID)
	}
	if trimmed == "" {
		return nil
	}

	// If session is busy with an active stream, mark it for pending resume
	// instead of dropping the request. It will be processed when the stream completes.
	if m.isSessionBusy(trimmed) {
		m.streamManager().MarkPendingAsyncResume(trimmed)
		return nil
	}

	// Additional safeguard: Don't resume if there are still pending tool calls.
	// This prevents cascading resumes when multiple async tasks complete in sequence.
	calls, _ := m.streamManager().PendingToolCalls(trimmed)
	if len(calls) > 0 {
		// There are still tools executing, so don't resume yet.
		// The resume will be triggered when those complete.
		return nil
	}

	m.beginPendingAssistant(trimmed)
	var cmds []tea.Cmd
	if trimmed == m.sessionID && m.messages != nil {
		m.messages.AddAssistantStart(llm.ModelName())
		if cmd := m.messages.StartLoading(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if streamCmd := m.requestLLM(trimmed); streamCmd != nil {
		cmds = append([]tea.Cmd{streamCmd}, cmds...)
	}
	if trimmed == m.sessionID {
		m.updateStatusForCurrentSession()
		m.refreshHeaderMeta()
		m.refreshHelp()
	}
	return batchCmds(cmds)
}

// ============================================================================
// Span Management
// ============================================================================

func (m *Model) beginSpanTurnForSession(sessionID string) {
	m.sessionManager().BeginSpan(sessionID)
}

func (m *Model) recordSpanIDForSession(sessionID, spanID string) {
	m.sessionManager().RecordSpanID(sessionID, spanID)
}

func (m *Model) parentSpanIDForSession(sessionID string) string {
	return m.sessionManager().ParentSpanID(sessionID)
}

func (m *Model) clearSpanForSession(sessionID string) {
	m.sessionManager().ClearSpan(sessionID)
}

// ============================================================================
// Building Instructions & Conversation
// ============================================================================

func (m *Model) buildInstructionsForSession(sessionID, basePrompt, agentName, agentPrompt string, agentOptions []sessionstate.AgentOption, agentListErr error) string {
	// Get focused agent tools and info
	var focusedAgentTools []tooling.Spec
	focusedAgentInfo := sessionstate.FocusedAgentInfo{}
	coreAgentID := m.currentCoreAgentID()

	if m.agents != nil {
		focusedAgentTools = m.agents.extraSpecsForSession()
		focusedAgentInfo.Name = m.agents.focusedAgent()
	}

	// Get focused agent description and todos from sidebar (only available in Builder mode)
	if m.sidebar != nil && coreAgentID == coreagent.IDBuilder {
		focusedAgentInfo.Description = m.sidebar.FocusedAgentDescription()
		sidebarTodos := m.sidebar.FocusedAgentTodos()
		focusedAgentInfo.Todos = make([]sessionstate.TodoInfo, len(sidebarTodos))
		for i, todo := range sidebarTodos {
			focusedAgentInfo.Todos[i] = sessionstate.TodoInfo{
				Text:      todo.Text,
				Completed: todo.Completed,
			}
		}
	}

	return sessionstate.BuildInstructions(basePrompt, agentName, agentPrompt, agentOptions, agentListErr, focusedAgentTools, focusedAgentInfo, coreAgentID)
}

func (m *Model) buildConversationForSession(sessionID string) []map[string]any {
	history := m.sessionManager().ConversationHistory(context.Background(), sessionID)
	return sessionstate.BuildConversation(history)
}

func (m *Model) lastAssistantContentForSession(sessionID string) string {
	return m.sessionManager().LastAssistantContent(context.Background(), sessionID)
}

func (m *Model) LastAssistantContent() string {
	return m.lastAssistantContentForSession(m.sessionID)
}

// ============================================================================
// Clear Conversation
// ============================================================================

func (m *Model) ClearConversation() {
	m.messages.LoadConversation(nil)
	if m.msgStore != nil {
		_ = m.msgStore.DeleteBySession(context.Background(), m.sessionID)
	}
	_ = tooling.DeleteAsyncTasksBySession(context.Background(), m.sessionID)
	_ = m.sessionManager().LoadSession(context.Background(), m.sessionID)
	m.clearSpanForSession(m.sessionID)

	if agentName := m.currentActiveAgentName(); agentName != "" {
		tooling.SendLifecycleEvent(agentName, "new_conversation", map[string]interface{}{
			"conversation_id": m.sessionID,
			"is_clear":        true,
		})
	}
}

// ============================================================================
// Session Switching
// ============================================================================

func (m *Model) setSession(id string) tea.Cmd {
	if id == m.sessionID {
		return nil
	}
	previousID := m.sessionID
	m.sessionID = id
	m.agentPicker = nil
	m.toolDetail = nil
	if m.agents != nil {
		m.agents.setSessionID(id)
		m.agents.resetActiveAgentState()
	}

	var messageCount int
	if m.msgStore != nil {
		if msgs, err := m.msgStore.List(context.Background(), id); err == nil {
			messageCount = len(msgs)
		}
	}

	alerts := m.restoreActiveAgentForSession(id)

	// Attempt to load conversation - if it fails, continue with an empty session
	// rather than crashing the application
	if err := m.loadConversation(id); err != nil {
		// TODO: Consider adding error notification to user
		// For now, silently continue with empty session
		_ = err
	}

	m.updateStatusForCurrentSession()
	m.refreshHeaderMeta()
	m.refreshHelp()
	_ = m.refreshSidebar()

	lifecycleCmd := func() tea.Msg {
		if agentName := m.currentActiveAgentName(); agentName != "" {
			if messageCount == 0 {
				tooling.SendLifecycleEvent(agentName, "new_conversation", map[string]interface{}{
					"conversation_id": id,
					"is_clear":        false,
				})
			} else {
				tooling.SendLifecycleEvent(agentName, "conversation_switched", map[string]interface{}{
					"conversation_id": id,
					"previous_id":     previousID,
					"message_count":   messageCount,
				})
			}
		}
		return nil
	}

	var cmds []tea.Cmd
	cmds = append(cmds, alerts...)
	cmds = append(cmds, lifecycleCmd)
	if cmd := m.restorePendingAssistant(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.restorePendingToolCalls(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if m.sidebar != nil && m.currentCoreAgentID() == coreagent.IDBuilder {
		if focusedAgent := m.sidebar.FocusedAgentName(); strings.TrimSpace(focusedAgent) != "" {
			cmds = append(cmds, m.fetchFocusedAgentMetadataCmd(focusedAgent))
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// ============================================================================
// Load Conversation
// ============================================================================

func (m *Model) loadConversation(sessionID string) error {
	msgs, err := m.msgStore.List(context.Background(), sessionID)
	if err != nil {
		return err
	}

	if err := m.sessionManager().LoadSession(context.Background(), sessionID); err != nil {
		return err
	}

	m.messages.LoadConversation(msgs)

	// Restore the focused agent from the conversation
	if m.convStore != nil && m.sidebar != nil && m.agents != nil {
		if conv, err := m.convStore.Get(context.Background(), sessionID); err == nil {
			// Update both sidebar and agent controller with focused agent
			m.sidebar.SetFocusedAgent(conv.FocusedAgentName)
			tooling.SetCurrentFocusedAgent(conv.FocusedAgentName)

			// Update agent controller so it can provide tools and metadata
			if conv.FocusedAgentName != "" {
				m.agents.setFocusedAgent(conv.FocusedAgentName)

				// Fetch agent metadata synchronously to populate description immediately
				// This ensures the description is available for the prompt on first load
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				if meta, err := llm.FetchAgentMetadata(ctx, conv.FocusedAgentName); err == nil {
					m.sidebar.SetFocusedAgentDescription(meta.Description)
				}
				cancel()
			} else {
				m.agents.clearFocusedAgent()
				m.sidebar.SetFocusedAgentDescription("")
			}

			// Set status from agentStatuses map if available (may not be populated yet during init)
			if conv.FocusedAgentName != "" {
				if status, ok := m.agentStatuses[conv.FocusedAgentName]; ok {
					m.sidebar.SetFocusedAgentStatus(status)
				} else {
					m.sidebar.SetFocusedAgentStatus("")
				}
			} else {
				m.sidebar.SetFocusedAgentStatus("")
			}
			m.sidebar.SetFocusedAgentCommands(nil)
			m.sidebar.SetAgentLogs(nil)
		}
	}

	return nil
}

func (m *Model) maybeUpdateConversationTitle(text string) {
	m.sessionManager().MaybeUpdateTitle(context.Background(), text)
}
