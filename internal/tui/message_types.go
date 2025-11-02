package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea/v2"

	"opperator/updater"
	cmpsidebar "tui/components/sidebar"
	"tui/internal/plan"
	"tui/internal/pubsub"
	llm "tui/llm"
	"tui/permission"
	"tui/secretprompt"
	sessionstate "tui/sessionstate"
	tooling "tui/tools"
)

// Message type definitions - extracted from model.go

type statsCountsMsg struct {
	running, stopped, crashed, total int
	err                              error
}

type initialStatsMsg struct {
	statuses                         map[string]string
	running, stopped, crashed, total int
}

type cancelTimerExpiredMsg struct{ SessionID string }

type cycleAgentResultMsg struct {
	err         error
	coreID      string
	meta        llm.AgentMetadata
	hasMeta     bool
	warn        string
	clearActive bool
}

type asyncTasksSnapshotMsg struct {
	tasks []tooling.AsyncTask
	err   error
}

type agentCommandResultMsg struct {
	agent   string
	command string
	output  string
	result  any
	err     error
	callID  string // If set, tool call was already added elsewhere (e.g., by slash command parser)
}

type slashCommandArgumentParsedMsg struct {
	agent      string
	command    string
	rawInput   string        // Raw input text from user
	args       map[string]any
	callID     string        // Track the tool call ID we already added
	err        error
	isAsync    bool          // Whether this is an async command
	toolName   string        // Generated tool name for proper invocation
}

type permissionRequestEventMsg struct {
	Event pubsub.Event[permission.PermissionRequest]
}

type permissionNotificationEventMsg struct {
	Event pubsub.Event[permission.PermissionNotification]
}

type secretPromptEventMsg struct {
	Event pubsub.Event[secretprompt.PromptRequest]
}

type focusAgentEventMsg struct {
	Event pubsub.Event[tooling.FocusAgentEvent]
}

type planEventMsg struct {
	Event pubsub.Event[tooling.PlanEvent]
}

type focusedAgentMetadataMsg struct {
	agentName string
	metadata  llm.AgentMetadata
	logs      []string
	err       error
}

type agentMetadataFetchedMsg struct {
	agentName string
	metadata  llm.AgentMetadata
	err       error
}

type agentListRefreshedMsg struct {
	agents []llm.AgentInfo
	err    error
}

type initialAgentLogsMsg struct {
	agentName string
	logs      []string
	err       error
}

type initialPlanItemsMsg struct {
	agentName string
	items     []plan.PlanItem
	err       error
}

type initialCustomSectionsMsg struct {
	agentName string
	sections  []cmpsidebar.CustomSection
	err       error
}

type permissionDialogReadyMsg struct {
	requestID string
}

type pendingAssistantState struct {
	content string
	waiting bool
}

type sessionStreamMsg struct {
	SessionID string
	Msg       tea.Msg
}

type updateCheckMsg struct {
	available bool
	err       error
}

// Type alias
type sessionAgentOption = sessionstate.AgentOption

// Event waiter functions - wait for events from various channels

func (m *Model) waitTaskWatcherUpdate() tea.Cmd {
	if m.asyncTaskWatcher == nil {
		return nil
	}
	ch := m.asyncTaskWatcher.Updates()
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		return <-ch
	}
}

func (m *Model) waitPermissionRequestEvent() tea.Cmd {
	if m.permissionReqCh == nil {
		return nil
	}
	return func() tea.Msg {
		evt, ok := <-m.permissionReqCh
		if !ok {
			return nil
		}
		return permissionRequestEventMsg{Event: evt}
	}
}

func (m *Model) waitFocusAgentEvent() tea.Cmd {
	if m.focusAgentCh == nil {
		return nil
	}
	return func() tea.Msg {
		evt, ok := <-m.focusAgentCh
		if !ok {
			return nil
		}
		return focusAgentEventMsg{Event: evt}
	}
}

func (m *Model) waitPlanEvent() tea.Cmd {
	if m.planCh == nil {
		return nil
	}
	return func() tea.Msg {
		evt, ok := <-m.planCh
		if !ok {
			return nil
		}
		return planEventMsg{Event: evt}
	}
}

func (m *Model) waitAgentStateEvent() tea.Cmd {
	if m.agentStateCh == nil {
		return nil
	}
	return func() tea.Msg {
		evt, ok := <-m.agentStateCh
		if !ok {
			return nil
		}
		return evt
	}
}

func (m *Model) waitPermissionNotificationEvent() tea.Cmd {
	if m.permissionNotifCh == nil {
		return nil
	}
	return func() tea.Msg {
		evt, ok := <-m.permissionNotifCh
		if !ok {
			return nil
		}
		return permissionNotificationEventMsg{Event: evt}
	}
}

func (m *Model) waitSecretPromptEvent() tea.Cmd {
	ch := m.SecretPromptController.promptCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return nil
		}
		return secretPromptEventMsg{Event: evt}
	}
}

func (m *Model) waitAsyncTaskUpdate() tea.Cmd {
	if m.llmEngine == nil {
		return nil
	}
	updates := m.llmEngine.AsyncUpdates()
	if updates == nil {
		return nil
	}
	return func() tea.Msg {
		update, ok := <-updates
		if !ok {
			return nil
		}
		return update
	}
}

func (m *Model) restoreAsyncTasksCmd() tea.Cmd {
	return func() tea.Msg {
		tasks, err := tooling.ListAsyncTasks(context.Background())
		if err != nil {
			return asyncTasksSnapshotMsg{err: err}
		}
		return asyncTasksSnapshotMsg{tasks: tasks}
	}
}

func (m *Model) checkForUpdatesCmd() tea.Cmd {
	return func() tea.Msg {
		info, err := updater.CheckForUpdates(false) // Don't include pre-releases in automatic checks
		if err != nil {
			// Silently fail - don't disrupt user experience if check fails
			return updateCheckMsg{available: false, err: err}
		}
		return updateCheckMsg{available: info.Available, err: nil}
	}
}
