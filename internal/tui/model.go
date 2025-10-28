package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/v2/help"
	tea "github.com/charmbracelet/bubbletea/v2"

	"tui/commands"
	cmpconversations "tui/components/conversations"
	cmpheader "tui/components/header"
	cmpinput "tui/components/input"
	cmpmessages "tui/components/messages"
	cmpsidebar "tui/components/sidebar"
	cmpstats "tui/components/stats"
	cmpstatus "tui/components/status"
	"tui/coreagent"
	"tui/internal/conversation"
	"tui/internal/inputhistory"
	"tui/internal/message"
	"tui/internal/plan"
	"tui/internal/preferences"
	"tui/internal/pubsub"
	llm "tui/llm"
	"tui/lsp"
	"tui/modelbuilder"
	"tui/permission"
	"tui/secretprompt"
	sessionstate "tui/sessionstate"
	streaming "tui/streaming"
	tooling "tui/tools"

	"tui/internal/protocol"
)

const (
	permissionDialogDelay = 1500 * time.Millisecond
)

var asyncToolRunner = tooling.RunAsyncTool

type UIComponents struct {
	header             cmpheader.Header
	messages           *cmpmessages.Messages
	input              *cmpinput.Input
	stats              *cmpstats.Stats
	sidebar            *cmpsidebar.Sidebar
	sidebarVisible     bool
	status             cmpstatus.StatusCmp
	convModal          *cmpconversations.Model
	agentPicker        *agentPicker
	agentPickerIsFocus bool // true if picker is for /focus command, false for /agent
	keys               keyMap
	help               help.Model
	helpH              int

	toolDetail *toolDetailOverlay
}

type SessionState struct {
	convStore  *conversation.Store
	msgStore   message.Service
	inputStore inputhistory.Service
	prefsStore *preferences.Store
	planStore  *plan.Store
	sessionID  string
	historyMgr *sessionstate.Manager
}

type StreamTracker struct {
	manager *streaming.Manager
}

type PermissionController struct {
	permissions           permission.Service
	permissionReqCh       <-chan pubsub.Event[permission.PermissionRequest]
	permissionNotifCh     <-chan pubsub.Event[permission.PermissionNotification]
	permissionReqCancel   context.CancelFunc
	permissionNotifCancel context.CancelFunc
	ui                    *permissionUI
	pendingRequests       map[string]permission.PermissionRequest
}

type SecretPromptController struct {
	prompts      secretprompt.Service
	promptCh     <-chan pubsub.Event[secretprompt.PromptRequest]
	promptCancel context.CancelFunc
	ui           *secretPromptUI
}

type Model struct {
	w, h int

	UIComponents
	SessionState
	StreamTracker
	PermissionController
	SecretPromptController

	llmEngine *llm.Engine

	workingDir     string
	userWorkingDir string

	lspManager *lsp.Manager

	agents *agentController

	keyHandlers       map[string]keyHandler
	asyncProgressSeen map[string]int
	asyncTaskWatcher  *AsyncTaskWatcher
	pendingAsyncTasks map[string]string // map[taskID]callID - tracks async tasks waiting for completion

	agentStatuses map[string]string // map[agentName]status (running, stopped, crashed)

	focusAgentCh     <-chan pubsub.Event[tooling.FocusAgentEvent]
	focusAgentCancel context.CancelFunc

	planCh     <-chan pubsub.Event[tooling.PlanEvent]
	planCancel context.CancelFunc

	agentStateCh     <-chan agentStateEventMsg
	agentStateCancel context.CancelFunc
}

func (m *Model) currentCoreAgentID() string {
	if m.agents == nil {
		return ""
	}
	return m.agents.coreAgentID()
}

// GetCurrentCoreAgentID is the public version of currentCoreAgentID for the commands.Context interface
func (m *Model) GetCurrentCoreAgentID() string {
	return m.currentCoreAgentID()
}

func (m *Model) currentCoreAgentName() string {
	if m.agents == nil {
		return ""
	}
	name, _, _, _ := m.agents.coreAgentMetadata()
	return name
}

func (m *Model) currentCoreAgentPrompt() string {
	if m.agents == nil {
		return ""
	}
	_, prompt, _, _ := m.agents.coreAgentMetadata()
	return prompt
}

func (m *Model) currentCoreAgentColor() string {
	if m.agents == nil {
		return ""
	}
	_, _, color, _ := m.agents.coreAgentMetadata()
	return color
}

func (m *Model) currentAgentIdentifier() string {
	if name := strings.TrimSpace(m.currentActiveAgentName()); name != "" {
		return name
	}
	id := strings.TrimSpace(m.currentCoreAgentID())
	if id == "" {
		id = coreagent.IDOpperator
	}
	return id
}

// ClearFocus clears the focused agent in builder mode
func (m *Model) ClearFocus() {
	tooling.PublishFocusAgentEvent("")
}

func (m *Model) currentCoreAgentTools() []tooling.Spec {
	if m.agents == nil {
		return nil
	}
	_, _, _, tools := m.agents.coreAgentMetadata()
	return tools
}

func (m *Model) currentActiveAgentName() string {
	if m.agents == nil {
		return ""
	}
	name, _, _ := m.agents.activeAgentMetadata()
	return name
}

func (m *Model) currentActiveAgentPrompt() string {
	if m.agents == nil {
		return ""
	}
	_, prompt, _ := m.agents.activeAgentMetadata()
	return prompt
}

func (m *Model) currentActiveAgentCommands() []protocol.CommandDescriptor {
	if m.agents == nil {
		return nil
	}
	_, _, cmds := m.agents.activeAgentMetadata()
	return cmds
}

func (m *Model) currentActiveAgentColor() string {
	if m.agents == nil {
		return ""
	}
	return m.agents.activeAgentColor()
}

func (m *Model) currentActiveAgentDescription() string {
	if m.agents == nil {
		return ""
	}
	return m.agents.activeAgentDescription()
}

func (m *Model) currentAgentDisplay() (string, string) {
	if m.agents == nil {
		return "Opperator", ""
	}

	trimmedActive := strings.TrimSpace(m.currentActiveAgentName())
	if trimmedActive != "" {
		displayName := trimmedActive
		color := strings.TrimSpace(m.currentActiveAgentColor())
		if def, ok := m.agents.findCoreAgentDefinition(trimmedActive); ok {
			if name := strings.TrimSpace(def.Name); name != "" {
				displayName = name
			}
			if color == "" {
				color = strings.TrimSpace(def.Color)
			}
		}
		if displayName == "" {
			displayName = "Opperator"
		}
		return displayName, color
	}

	displayName := strings.TrimSpace(m.currentCoreAgentName())
	if displayName == "" {
		displayName = "Opperator"
	}
	color := strings.TrimSpace(m.currentCoreAgentColor())
	if def, ok := m.agents.findCoreAgentDefinition(strings.TrimSpace(m.currentCoreAgentID())); ok {
		if name := strings.TrimSpace(def.Name); name != "" {
			displayName = name
		}
		if color == "" {
			color = strings.TrimSpace(def.Color)
		}
	}
	return displayName, color
}

func New() (*Model, error) {
	deps, err := modelbuilder.New().Build()
	if err != nil {
		return nil, err
	}

	sidebarVisible := true
	if deps.PreferencesStore != nil {
		if visible, err := deps.PreferencesStore.GetBool(context.Background(), "sidebar.visible"); err == nil {
			sidebarVisible = visible
		}
	}

	m := &Model{
		UIComponents: UIComponents{
			header:         cmpheader.New(),
			messages:       &cmpmessages.Messages{},
			input:          &cmpinput.Input{},
			stats:          &cmpstats.Stats{},
			sidebar:        cmpsidebar.New(deps.PreferencesStore),
			sidebarVisible: sidebarVisible,
			status:         cmpstatus.NewStatusCmp(),
			keys:           defaultKeys,
			help:           help.New(),
		},
		SessionState: SessionState{
			convStore:  deps.ConversationStore,
			msgStore:   deps.MessageStore,
			inputStore: deps.InputStore,
			prefsStore: deps.PreferencesStore,
			planStore:  deps.PlanStore,
			sessionID:  deps.SessionID,
			historyMgr: sessionstate.NewManager(deps.ConversationStore, deps.MessageStore, deps.InputStore),
		},
		StreamTracker: StreamTracker{manager: streaming.NewManager()},
		PermissionController: PermissionController{
			permissions:     deps.PermissionService,
			pendingRequests: make(map[string]permission.PermissionRequest),
		},
		SecretPromptController: SecretPromptController{
			prompts: deps.SecretPromptService,
		},
		llmEngine:      deps.LLMEngine,
		workingDir:     deps.WorkingDir,
		userWorkingDir: deps.InvocationDir,
		lspManager:     deps.LSPManager,
	}
	m.asyncProgressSeen = make(map[string]int)
	m.pendingAsyncTasks = make(map[string]string)
	m.agentStatuses = make(map[string]string)

	if deps.ConversationStore != nil {
		m.asyncTaskWatcher = NewAsyncTaskWatcher(deps.ConversationStore.DB())
		m.asyncTaskWatcher.Start(context.Background())
	}

	m.agents = newAgentController(
		deps.ConversationStore,
		func() { m.refreshHeaderMeta() },
		func() { m.refreshHelp() },
		func() { m.refreshConversationModalList() },
		func() tea.Cmd { return m.refreshSidebar() },
		func(agentID string) { m.input.SetCurrentAgentID(agentID) },
	)
	m.agents.setSessionID(deps.SessionID)
	m.agents.setCoreAgent(coreagent.IDOpperator)

	m.permissionReqCancel = deps.PermissionRequestStop
	m.permissionNotifCancel = deps.PermissionNotifStop
	m.permissionReqCh = deps.PermissionRequestCh
	m.permissionNotifCh = deps.PermissionNotifCh
	m.SecretPromptController.promptCancel = deps.SecretPromptStop
	m.SecretPromptController.promptCh = deps.SecretPromptCh
	m.focusAgentCancel = deps.FocusAgentStop
	m.focusAgentCh = deps.FocusAgentCh
	m.planCancel = deps.PlanStop
	m.planCh = deps.PlanCh

	m.PermissionController.ui = newPermissionUI(permissionCallbacks{
		grant: func(req permission.PermissionRequest, persistent bool) {
			m.grantPermission(req, persistent)
		},
		deny: func(req permission.PermissionRequest) {
			if m.permissions != nil {
				m.permissions.Deny(req)
			}
		},
		cancelSession: m.cancelSessionFlow,
		focusInput: func() tea.Cmd {
			if m.input == nil {
				return nil
			}
			return m.input.Focus()
		},
		blurInput: func() tea.Cmd {
			if m.input == nil {
				return nil
			}
			return m.input.Blur()
		},
	})

	m.SecretPromptController.ui = newSecretPromptUI(secretPromptCallbacks{
		submit: func(req secretprompt.PromptRequest, value string) {
			if svc := m.secretPromptService(); svc != nil {
				svc.Resolve(req.ID, value)
			}
		},
		cancel: func(req secretprompt.PromptRequest) {
			if svc := m.secretPromptService(); svc != nil {
				svc.Reject(req.ID, secretprompt.ErrCanceled)
			}
		},
		focusInput: func() tea.Cmd {
			if m.input == nil {
				return nil
			}
			return m.input.Focus()
		},
		blurInput: func() tea.Cmd {
			if m.input == nil {
				return nil
			}
			return m.input.Blur()
		},
	})

	_ = m.agents.restoreActiveAgentForSession(deps.SessionID)

	if err := m.loadConversation(deps.SessionID); err != nil {
		return nil, fmt.Errorf("failed to load conversation: %w", err)
	}

	m.keyHandlers = defaultKeyHandlers()

	return m, nil
}

func (m *Model) Init() tea.Cmd {
	// Initialize agent state watcher (starts background goroutine)
	m.initAgentStateWatcher()

	cmds := []tea.Cmd{
		m.input.Init(),
		m.status.Init(),
		tea.EnableMouseAllMotion,
		// Polling disabled for logs/metadata - now using event-driven updates
	}

	cmds = append(cmds, m.initialStatsCmd())

	if cmd := m.waitPermissionRequestEvent(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.waitPermissionNotificationEvent(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.waitSecretPromptEvent(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.waitFocusAgentEvent(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.waitPlanEvent(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.waitAgentStateEvent(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.waitAsyncTaskUpdate(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.restoreAsyncTasksCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.waitTaskWatcherUpdate(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Check for updates in the background
	cmds = append(cmds, m.checkForUpdatesCmd())
	if m.sidebar != nil && m.currentCoreAgentID() == coreagent.IDBuilder {
		if focusedAgent := m.sidebar.FocusedAgentName(); strings.TrimSpace(focusedAgent) != "" {
			cmds = append(cmds, m.fetchFocusedAgentMetadataCmd(focusedAgent))
		}
	}

	// Fetch initial logs and custom sections for the active agent on startup
	if activeName := m.currentActiveAgentName(); strings.TrimSpace(activeName) != "" {
		cmds = append(cmds, m.fetchInitialAgentLogsCmd(activeName))
		cmds = append(cmds, m.fetchInitialCustomSectionsCmd(activeName))
	}

	return tea.Batch(cmds...)
}

func (m *Model) extraToolSpecsForSession() []tooling.Spec {
	if m.agents == nil {
		return nil
	}
	return m.agents.extraSpecsForSession()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if taskMsg, ok := msg.(TaskUpdateMsg); ok {
		// Check for completed tasks from slash commands
		if cmd := m.handleSlashCommandAsyncCompletion(taskMsg); cmd != nil {
			return m, tea.Batch(cmd, m.waitTaskWatcherUpdate())
		}
		// Async tasks are no longer displayed in the sidebar
		return m, m.waitTaskWatcherUpdate()
	}

	_, statusCmd := m.status.Update(msg)

	if cmd, handled := m.handleSecretPromptMsg(msg); handled {
		return m, tea.Batch(cmd, statusCmd)
	}

	if cmd, handled := m.handlePermissionOverlayMsg(msg); handled {
		return m, tea.Batch(cmd, statusCmd)
	}

	toolCmd, handledDetail := m.handleToolDetailMsg(msg)
	if handledDetail {
		return m, tea.Batch(toolCmd, statusCmd)
	}

	if cmd, handled := m.handleConvModalMsg(msg); handled {
		return m, tea.Batch(cmd, statusCmd)
	}

	if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.handleWindowSizeMsg(wsMsg)
	}

	if cmd, handled := m.handleKeyEvent(msg); handled {
		return m, tea.Batch(cmd, statusCmd)
	}

	componentCmd := m.updateInputAndMessages(msg)
	extraCmd := m.handleMessage(msg)

	return m, tea.Batch(componentCmd, extraCmd, statusCmd, m.overlayHelpCmd(), toolCmd)
}

func (m *Model) handleToolDetailMsg(msg tea.Msg) (tea.Cmd, bool) {
	if m.toolDetail == nil {
		return nil, false
	}

	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.toolDetail.SetSize(v.Width, v.Height)
		return nil, false
	case tea.KeyMsg, tea.KeyPressMsg:
		keyStr, ok := keyString(msg)
		if ok && keyStr == "esc" {
			cmd := m.closeToolDetail()
			return cmd, true
		}
		return m.toolDetail.Update(msg), true
	case tea.MouseMsg:
		return m.toolDetail.Update(msg), true
	default:
		cmd := m.toolDetail.Update(msg)
		return cmd, false
	}
}

func (m *Model) handleMessage(msg tea.Msg) tea.Cmd {
	switch v := msg.(type) {
	case sessionStreamMsg:
		return m.handleStreamMsg(v.SessionID, v.Msg)
	case initialStatsMsg:
		m.agentStatuses = v.statuses
		if m.stats != nil {
			m.stats.SetProcessCounts(v.running, v.stopped, v.crashed, v.total)
			m.stats.SetError(nil)
		}
		// Update focused agent status in Builder mode if we have one
		if m.sidebar != nil && m.currentCoreAgentID() == coreagent.IDBuilder {
			if focusedAgent := m.sidebar.FocusedAgentName(); focusedAgent != "" {
				if status, ok := v.statuses[focusedAgent]; ok {
					m.sidebar.SetFocusedAgentStatus(status)
				}
			}
		}
		return m.nextStreamCmd()
	case statsCountsMsg:
		if v.err != nil {
			m.stats.SetError(v.err)
		} else {
			m.stats.SetProcessCounts(v.running, v.stopped, v.crashed, v.total)
			m.stats.SetError(nil)
		}
		return m.nextStreamCmd()
	case updateCheckMsg:
		// Update the header to show update notification
		if v.available && m.header != nil {
			m.header.SetUpdateAvailable(true)
		}
		return m.nextStreamCmd()
	case agentStateEventMsg:
		currentAgent := strings.TrimSpace(m.currentActiveAgentName())
		coreID := strings.TrimSpace(m.currentCoreAgentID())

		if v.Type == "status" && v.Status != "" {
			m.updateAgentStatusAndRefreshStats(v.AgentName, v.Status)

			// Update agent list in Opperator mode sidebar
			if m.sidebar != nil && coreID == coreagent.IDOpperator {
				m.refreshAgentListInSidebar()
			}

			// Track if we need to fetch metadata when agent starts
			shouldFetchMetadata := false
			isFocusedAgentInBuilder := false

			// Update focused agent status in Builder mode
			if m.sidebar != nil && coreID == coreagent.IDBuilder {
				focusedName := m.sidebar.FocusedAgentName()
				if v.AgentName == focusedName && strings.TrimSpace(focusedName) != "" {
					m.sidebar.SetFocusedAgentStatus(v.Status)
					isFocusedAgentInBuilder = true

					// Agent just started - fetch metadata
					if v.Status == "running" {
						shouldFetchMetadata = true
					}
				}
			}

			// Refresh tool specs when any agent restarts (transitions to running)
			// This ensures the latest commands are available in the current session
			if v.Status == "running" && strings.TrimSpace(v.AgentName) != "" && !isFocusedAgentInBuilder {
				shouldFetchMetadata = true
			}

			if shouldFetchMetadata {
				return tea.Batch(
					m.fetchFocusedAgentMetadataCmd(v.AgentName),
					m.waitAgentStateEvent(),
				)
			}
		}

		// Process events based on type - removed restrictive outer filter
		// to allow events to be processed even before agent is fully initialized
		if m.sidebar != nil {
			isCurrentAgent := v.AgentName == currentAgent

			// In Builder mode, also check if this is the focused agent
			isFocusedAgent := false
			if coreID == coreagent.IDBuilder {
				focusedName := m.sidebar.FocusedAgentName()
				isFocusedAgent = (focusedName != "" && v.AgentName == focusedName)
			}

			switch v.Type {
			case "sections":
				// Set custom sections if this is the current active agent OR focused agent in Builder
				if v.CustomSections != nil && (isCurrentAgent || isFocusedAgent) {
					m.sidebar.SetCustomSections(v.CustomSections)
				}
			case "commands":
				agentName := strings.TrimSpace(v.AgentName)
				if agentName != "" && v.Commands != nil {
					normalized := protocol.NormalizeCommandDescriptors(v.Commands)
					tooling.BuildAgentCommandTools(agentName, normalized)
					if m.agents != nil {
						m.agents.updateActiveAgentCommands(agentName, normalized)
					}
					if coreID == coreagent.IDBuilder {
						focusedName := strings.TrimSpace(m.sidebar.FocusedAgentName())
						if focusedName != "" && strings.EqualFold(focusedName, agentName) {
							m.sidebar.SetFocusedAgentCommands(normalized)
						}
					}
				}
			case "logs":
				// Determine if we should display these logs based on current mode
				shouldProcessLogs := false
				if coreID == coreagent.IDBuilder {
					// In Builder mode, show logs from the focused agent OR active agent
					// (active agent can run without being explicitly focused)
					focusedName := m.sidebar.FocusedAgentName()
					if (focusedName != "" && v.AgentName == focusedName) || (focusedName == "" && isCurrentAgent) {
						shouldProcessLogs = true
					}
				} else {
					// In other modes, show logs from the current active agent
					if isCurrentAgent {
						shouldProcessLogs = true
					}
				}

				if shouldProcessLogs {
					// Handle bulk log updates (initial load)
					if v.Logs != nil {
						m.sidebar.SetAgentLogs(v.Logs)
					}
					// Handle single log entry append (streaming updates)
					if v.LogEntry != "" {
						m.sidebar.AppendAgentLog(v.LogEntry)
					}
				}
			case "metadata":
				agentName := strings.TrimSpace(v.AgentName)
				description := strings.TrimSpace(v.Description)
				systemPrompt := strings.TrimSpace(v.SystemPrompt)
				color := strings.TrimSpace(v.Color)

				if m.agents != nil {
					m.agents.updateActiveAgentMetadata(agentName, description, systemPrompt, color)
				}
				if m.sidebar != nil {
					if agentName != "" && strings.EqualFold(agentName, currentAgent) {
						_, _ = m.sidebar.SetAgentInfo(agentName, description, color, m.currentActiveAgentCommands())
					}
					if coreID == coreagent.IDBuilder {
						focusedName := strings.TrimSpace(m.sidebar.FocusedAgentName())
						if focusedName != "" && strings.EqualFold(focusedName, agentName) {
							m.sidebar.SetFocusedAgentDescription(description)
							m.sidebar.SetFocusedAgentColor(color)
						}
					}
					// Refresh agent list in Opperator mode to update colors
					if coreID == coreagent.IDOpperator {
						m.refreshAgentListInSidebar()
						return tea.Batch(m.refreshSidebar(), m.waitAgentStateEvent())
					}
				}
			}
		}
		return m.waitAgentStateEvent()
	case cycleAgentResultMsg:
		return m.handleCycleAgentResult(v)
	case agentCommandResultMsg:
		return m.handleAgentCommandResult(v)
	case slashCommandArgumentParsedMsg:
		return m.handleSlashCommandArgumentParsed(v)
	case asyncTasksSnapshotMsg:
		return m.handleAsyncTasksSnapshot(v)
	case llm.AsyncToolUpdateMsg:
		return m.handleAsyncToolUpdate(v)
	case cancelTimerExpiredMsg:
		if state := m.streamState(v.SessionID); state != nil && state.Canceling {
			state.Canceling = false
			if v.SessionID == m.sessionID {
				m.refreshHelp()
			}
		}
		return nil
	case permissionRequestEventMsg:
		return m.handlePermissionRequestEvent(v)
	case permissionNotificationEventMsg:
		return m.handlePermissionNotificationEvent(v)
	case permissionDialogReadyMsg:
		return m.showPendingPermissionRequest(v.requestID)
	case secretPromptEventMsg:
		return m.handleSecretPromptEvent(v)
	case focusAgentEventMsg:
		return m.handleFocusAgentEvent(v)
	case planEventMsg:
		return m.handlePlanEvent(v)
	case focusedAgentMetadataMsg:
		return m.handleFocusedAgentMetadata(v)
	case initialAgentLogsMsg:
		return m.handleInitialAgentLogs(v)
	case initialPlanItemsMsg:
		return m.handleInitialPlanItems(v)
	case initialCustomSectionsMsg:
		return m.handleInitialCustomSections(v)
	default:
		return nil
	}
}

func (m *Model) submitInput(val string) tea.Cmd {
	if val == "" || m.isSessionBusy(m.sessionID) {
		return nil
	}
	if strings.HasPrefix(val, "/agent") {
		return m.handleAgentCommand(val)
	}
	if strings.HasPrefix(val, "/focus") {
		return m.handleFocusCommand(val)
	}
	m.agentPicker = nil
	if handled, cmd := commands.Execute(val, m); handled {
		m.input.SetValue("")
		return cmd
	}
	m.messages.AddUser(val)
	m.sessionManager().AppendInput(context.Background(), m.sessionID, val)
	m.addUserHistory(val)
	m.beginPendingAssistant(m.sessionID)
	m.messages.AddAssistantStart(llm.ModelName())
	m.input.SetValue("")
	m.streamManager().ClearToolTracking(m.sessionID)
	streamCmd := m.requestLLM(m.sessionID)
	m.updateStatusForCurrentSession()
	m.refreshHeaderMeta()
	m.refreshHelp()
	return tea.Batch(streamCmd, m.messages.StartLoading())
}

func (m *Model) baseToolSpecsForSession() []tooling.Spec {
	if m.agents == nil {
		return nil
	}
	return m.agents.baseSpecsForSession()
}

func (m *Model) clearActiveAgent() {
	if m.agents == nil {
		return
	}
	m.agents.clearActiveAgent()
}

func (m *Model) applyActiveAgent(meta llm.AgentMetadata, persist bool) tea.Cmd {
	if m.agents == nil {
		return nil
	}
	m.agents.applyActiveAgent(meta, persist)

	// Fetch initial logs and custom sections once when agent becomes active
	// Subsequent updates come via events
	return tea.Batch(
		m.fetchInitialAgentLogsCmd(meta.Name),
		m.fetchInitialCustomSectionsCmd(meta.Name),
	)
}

func (m *Model) restoreActiveAgentForSession(sessionID string) []tea.Cmd {
	if m.agents == nil {
		m.refreshHeaderMeta()
		m.refreshHelp()
		return nil
	}
	cmds := m.agents.restoreActiveAgentForSession(sessionID)

	// Fetch initial logs and custom sections once for restored active agent
	// Subsequent updates come via events
	if activeName := m.currentActiveAgentName(); strings.TrimSpace(activeName) != "" {
		cmds = append(cmds, m.fetchInitialAgentLogsCmd(activeName))
		cmds = append(cmds, m.fetchInitialCustomSectionsCmd(activeName))
	}

	return cmds
}

func (m *Model) layout() {
	layoutSimple(m)
}
