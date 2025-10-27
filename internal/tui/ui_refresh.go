package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	cmpconversations "tui/components/conversations"
	cmpsidebar "tui/components/sidebar"
	"tui/coreagent"
	llm "tui/llm"
	"tui/styles"
	tooling "tui/tools"
	tooltypes "tui/tools/types"
)

// currentKeys builds the dynamic key map based on current UI state
func (m *Model) currentKeys() dynamicKeyMap {
	km := m.keys
	theme := styles.CurrentTheme()
	muted := lipgloss.NewStyle().Foreground(colorToLipgloss(theme.FgMuted))
	mutedMore := lipgloss.NewStyle().Foreground(colorToLipgloss(theme.FgMutedMore))
	quitHelp := mutedMore.Render("quit")
	quitHelp += muted.Render(" • ")
	displayName, color := m.currentAgentDisplay()
	accent := agentColorOrSecondary(color, theme.Secondary)
	style := lipgloss.NewStyle().Foreground(accent)
	quitHelp += style.Bold(true).Render("∴ " + displayName)
	km.Quit = key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", quitHelp),
	)
	if m.toolDetail != nil {
		km.Cancel = key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close tool detail"),
		)
		return dynamicKeyMap{km: km, inputFocused: false, cancelVisible: true}
	}
	busy := m.isSessionBusy(m.sessionID)
	if busy {
		help := "cancel"
		if m.isCancelingSession(m.sessionID) {
			help = "press again to cancel"
		}
		km.Cancel = key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", help),
		)
	}
	return dynamicKeyMap{km: km, inputFocused: m.input.IsFocused(), cancelVisible: busy}
}

// refreshHelp updates the help/status bar and triggers layout if height changed
func (m *Model) refreshHelp() {
	m.status.SetKeyMap(m.currentKeys())
	newHelpH := lipgloss.Height(m.status.View())
	if newHelpH != m.helpH {
		m.helpH = newHelpH
		m.layout()
		return
	}
	m.helpH = newHelpH
}

// refreshHeaderMeta updates the header with current agent and session state
func (m *Model) refreshHeaderMeta() {
	status := "Ready"
	if m.isSessionBusy(m.sessionID) {
		status = "Typing…"
	}
	hint := ""
	agentDisplayName, agentColor := m.currentAgentDisplay()
	agentID := m.currentAgentIdentifier()
	var title string
	activeName := strings.TrimSpace(m.currentActiveAgentName())
	if activeName != "" {
		title = fmt.Sprintf("Agent: %s", activeName)
		toolCount, slashCount := countCommandExposures(m.currentActiveAgentCommands())
		switch {
		case toolCount == 0 && slashCount == 0:
			hint = fmt.Sprintf("0 commands • %s", m.currentCoreAgentName())
		default:
			parts := make([]string, 0, 2)
			if toolCount > 0 {
				parts = append(parts, fmt.Sprintf("%d tool(s)", toolCount))
			}
			if slashCount > 0 {
				parts = append(parts, fmt.Sprintf("%d slash", slashCount))
			}
			hint = fmt.Sprintf("%s • %s", strings.Join(parts, " • "), m.currentCoreAgentName())
		}
	} else {
		title = m.currentCoreAgentName()
		if strings.TrimSpace(title) == "" {
			title = "Opperator"
		}
		if count := len(m.currentCoreAgentTools()); count > 0 {
			hint = fmt.Sprintf("%d tool(s)", count)
		}
		if color := strings.TrimSpace(m.currentCoreAgentColor()); color != "" {
			title = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true).Render(title)
		}
	}
	if m.messages != nil {
		m.messages.SetAssistantDefaults(agentID, agentDisplayName, agentColor)
	}
	if m.header != nil {
		m.header.SetMeta(title, llm.ModelName(), status, hint)
	}
}

// refreshSidebar updates sidebar content based on current agent state
// Returns a command to fetch logs/sections if the agent changed
func (m *Model) refreshSidebar() tea.Cmd {
	if m.sidebar == nil {
		return nil
	}

	activeName := strings.TrimSpace(m.currentActiveAgentName())
	coreID := strings.TrimSpace(m.currentCoreAgentID())
	if coreID == "" {
		coreID = coreagent.IDOpperator
	}

	if activeName != "" {
		// Show the active agent's name, description, color, and commands
		description := m.currentActiveAgentDescription()
		color := m.currentActiveAgentColor()
		commands := m.currentActiveAgentCommands()
		_, agentNameChanged := m.sidebar.SetAgentInfo(activeName, description, color, commands)

		// If the agent name changed, fetch logs and custom sections for the new agent
		if agentNameChanged {
			return tea.Batch(
				m.fetchInitialAgentLogsCmd(activeName),
				m.fetchInitialCustomSectionsCmd(activeName),
			)
		}
		// Custom sections are now updated via events only (no polling)
	} else {
		if coreID == coreagent.IDOpperator {
			// Show Opperator info in sidebar
			coreName := m.currentCoreAgentName()
			coreColor := m.currentCoreAgentColor()

			description := "Core agent for managing and orchestrating other agents"

			// Core agents don't have commands in the same way active agents do,
			// so we pass nil for commands
			_, _ = m.sidebar.SetAgentInfo(coreName, description, coreColor, nil)

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if agents, err := llm.ListAgents(ctx); err == nil && len(agents) > 0 {
				agentList := make([]cmpsidebar.AgentListItem, 0, len(agents))
				for _, agent := range agents {
					agentList = append(agentList, cmpsidebar.AgentListItem{
						Name:        agent.Name,
						Description: agent.Description,
						Status:      agent.Status,
						Color:       agent.Color,
					})
				}
				m.sidebar.SetAgentList(agentList)
			} else {
				m.sidebar.SetAgentList(nil)
			}

			m.sidebar.SetCustomSections(nil)
		} else if coreID == coreagent.IDBuilder {
			// Show Builder info in sidebar
			coreName := m.currentCoreAgentName()
			coreColor := m.currentCoreAgentColor()

			description := "Build and customize specialized agents designed for your specific needs. The Builder agent lets you compose custom agents by selecting tools, defining behaviors, and configuring capabilities to handle particular tasks or domains. Create agents optimized for database work, API testing, code generation, or any workflow that benefits from specialized automation and intelligence."

			// Core agents don't have commands in the same way active agents do,
			// so we pass nil for commands
			_, _ = m.sidebar.SetAgentInfo(coreName, description, coreColor, nil)

			// Don't clear focused agent here - it's managed independently:
			// - Set by loadConversation when switching conversations (restores from DB)
			// - Cleared/set by focus_agent tool (via handleFocusAgentEvent)

			// Similarly, don't clear custom sections if there's a focused agent
			// Custom sections for focused agents are fetched via handleFocusedAgentMetadata
			if focusedAgent := m.sidebar.FocusedAgentName(); focusedAgent == "" {
				m.sidebar.SetCustomSections(nil)
			}
		} else {
			_, _ = m.sidebar.SetAgentInfo("", "", "", nil)
			m.sidebar.SetCustomSections(nil)
			m.sidebar.SetAgentList(nil)
			m.sidebar.SetFocusedAgent("")
		}
	}

	return nil
}

// handleWindowSizeMsg updates all UI components when window size changes
func (m *Model) handleWindowSizeMsg(msg tea.WindowSizeMsg) {
	m.w, m.h = msg.Width, msg.Height
	m.help.Width = msg.Width
	layoutSimple(m)
	m.refreshHelp()
	m.refreshHeaderMeta()
	if m.agentPicker != nil {
		m.agentPicker.SetMaxWidth(m.agentPickerMaxWidth())
	}
	if ui := m.permissionUI(); ui != nil && ui.active() {
		m.updatePermissionOverlaySize()
	}
	if ui := m.secretPromptUI(); ui != nil && ui.active() {
		m.updateSecretPromptOverlaySize()
	}
	if m.toolDetail != nil {
		m.toolDetail.SetSize(msg.Width, msg.Height)
	}
}

// openToolDetail opens the tool detail overlay for the given call/result
func (m *Model) openToolDetail(call tooltypes.Call, result tooltypes.Result) tea.Cmd {
	id := strings.TrimSpace(call.ID)
	if id == "" {
		return nil
	}
	if m.toolDetail == nil {
		m.toolDetail = newToolDetailOverlay(call, result, m.w, m.h)
	} else {
		m.toolDetail.SetSize(m.w, m.h)
		if cmd := m.toolDetail.SetData(call, result); cmd != nil {
			m.refreshHelp()
			return cmd
		}
	}
	m.refreshHelp()
	return nil
}

// closeToolDetail closes the tool detail overlay
func (m *Model) closeToolDetail() tea.Cmd {
	if m.toolDetail == nil {
		return nil
	}
	m.toolDetail = nil
	m.refreshHelp()
	if !m.input.IsFocused() {
		return m.input.Focus()
	}
	return nil
}

// refreshToolDetail updates the tool detail overlay with latest data
func (m *Model) refreshToolDetail(id string) tea.Cmd {
	if m.toolDetail == nil || m.messages == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" || strings.TrimSpace(m.toolDetail.CallID()) != id {
		return nil
	}
	call, result, ok := m.messages.ToolCallByID(id)
	if !ok {
		return nil
	}
	return m.toolDetail.SetData(call, result)
}

// handleConvModalMsg handles conversation modal events
func (m *Model) handleConvModalMsg(msg tea.Msg) (tea.Cmd, bool) {
	if m.convModal == nil {
		return nil, false
	}

	switch v := msg.(type) {
	case cmpconversations.SelectedMsg:
		m.convModal = nil
		cmd := m.setSession(v.ID)
		m.refreshHeaderMeta()
		return tea.Batch(m.input.Focus(), cmd), true
	case cmpconversations.NewMsg:
		conv, _ := m.convStore.Create(context.Background(), "")
		convs, _ := m.convStore.List(context.Background())
		m.convModal.SetConversations(convs)
		m.convModal = nil
		cmd := m.setSession(conv.ID)
		m.refreshHeaderMeta()
		return tea.Batch(m.input.Focus(), cmd), true
	case cmpconversations.DeleteMsg:
		m.clearSpanForSession(v.ID)

		var msgCount int
		if m.msgStore != nil {
			if msgs, err := m.msgStore.List(context.Background(), v.ID); err == nil {
				msgCount = len(msgs)
			}
		}

		if agentName := m.currentActiveAgentName(); agentName != "" {
			tooling.SendLifecycleEvent(agentName, "conversation_deleted", map[string]interface{}{
				"conversation_id": v.ID,
				"message_count":   msgCount,
			})
		}

		if m.msgStore != nil {
			_ = m.msgStore.DeleteBySession(context.Background(), v.ID)
		}
		_ = tooling.DeleteAsyncTasksBySession(context.Background(), v.ID)
		_ = m.convStore.Delete(context.Background(), v.ID)
		m.convModal = nil
		convs, _ := m.convStore.List(context.Background())
		if len(convs) > 0 {
			cmd := m.setSession(convs[0].ID)
			m.refreshHeaderMeta()
			return tea.Batch(m.input.Focus(), cmd), true
		} else {
			c, _ := m.convStore.Create(context.Background(), "")
			cmd := m.setSession(c.ID)
			m.refreshHeaderMeta()
			return tea.Batch(m.input.Focus(), cmd), true
		}
	case cmpconversations.CloseMsg:
		m.convModal = nil
		return m.input.Focus(), true
	case tea.WindowSizeMsg:
		cmd := m.updateConvModal(msg)
		m.handleWindowSizeMsg(v)
		return cmd, true
	case tea.KeyMsg, tea.KeyPressMsg, tea.MouseMsg:
		return m.updateConvModal(msg), true
	default:
		return nil, false
	}
}

// updateConvModal forwards update messages to the conversation modal
func (m *Model) updateConvModal(msg tea.Msg) tea.Cmd {
	if m.convModal == nil {
		return nil
	}

	mdl, cmd := m.convModal.Update(msg)
	if cm, ok := mdl.(*cmpconversations.Model); ok {
		m.convModal = cm
	}
	return cmd
}

// refreshConversationModalList refreshes the conversation list in the modal
func (m *Model) refreshConversationModalList() {
	if m.convModal == nil || m.convStore == nil {
		return
	}
	if convs, err := m.convStore.List(context.Background()); err == nil {
		m.convModal.SetConversations(convs)
	}
}

// overlayHelpCmd returns a command that updates help based on active overlay
func (m *Model) overlayHelpCmd() tea.Cmd {
	if m.status == nil {
		return nil
	}
	permUI := m.permissionUI()
	secretUI := m.secretPromptUI()
	return func() tea.Msg {
		switch {
		case secretUI != nil && secretUI.active():
			m.status.SetKeyMap(secretPromptHelpKeyMap{})
		case permUI != nil && permUI.active():
			m.status.SetKeyMap(permissionHelpKeyMap{})
		default:
			m.status.SetKeyMap(m.currentKeys())
		}
		return nil
	}
}

// updatePermissionOverlaySize updates permission overlay dimensions
func (m *Model) updatePermissionOverlaySize() {
	ui := m.permissionUI()
	if ui == nil {
		return
	}
	width := m.w - 4
	if m.input != nil {
		width = m.w - m.input.CommandPickerXOffset() - 4
	}
	if width < 50 {
		width = 50
	}
	if width > m.w-2 {
		width = m.w - 2
	}
	ui.SetWidth(width)
}

// updateSecretPromptOverlaySize updates secret prompt overlay dimensions
func (m *Model) updateSecretPromptOverlaySize() {
	ui := m.secretPromptUI()
	if ui == nil {
		return
	}
	width := m.w - 4
	if m.input != nil {
		width = m.w - m.input.CommandPickerXOffset() - 4
	}
	if width < 48 {
		width = 48
	}
	if width > m.w-2 {
		width = m.w - 2
	}
	ui.SetWidth(width)
}

// updateInputAndMessages forwards updates to input and messages components
func (m *Model) updateInputAndMessages(msg tea.Msg) tea.Cmd {
	var selectionCmd tea.Cmd
	keyConsumed := false

	if m.messages != nil {
		switch km := msg.(type) {
		case tea.KeyPressMsg:
			selectionCmd, keyConsumed = m.messages.HandleSelectionKeyPress(km)
		case tea.KeyMsg:
			if keyPress, ok := km.(tea.KeyPressMsg); ok {
				selectionCmd, keyConsumed = m.messages.HandleSelectionKeyPress(keyPress)
			}
		}
	}

	var cmdInput tea.Cmd
	if !keyConsumed && m.input != nil {
		cmdInput = m.input.Update(msg)
	}

	agentCmd := m.refreshAgentPickerFromInput()

	var cmdSidebar tea.Cmd
	if m.sidebar != nil {
		cmdSidebar = m.sidebar.Update(msg)
	}

	var cmdMsgs tea.Cmd
	if m.messages != nil && !m.shouldSkipMessagesUpdate(msg) {
		cmdMsgs = m.messages.Update(msg)
	}

	var cmds []tea.Cmd
	if cmdInput != nil {
		cmds = append(cmds, cmdInput)
	}
	if cmdMsgs != nil {
		cmds = append(cmds, cmdMsgs)
	}
	if selectionCmd != nil {
		cmds = append(cmds, selectionCmd)
	}
	if agentCmd != nil {
		cmds = append(cmds, agentCmd)
	}
	if cmdSidebar != nil {
		cmds = append(cmds, cmdSidebar)
	}

	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// shouldSkipMessagesUpdate determines if messages component should skip this update
func (m *Model) shouldSkipMessagesUpdate(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.KeyMsg, tea.KeyPressMsg:
		if !m.input.IsFocused() {
			return false
		}
		if m.messages != nil && m.messages.HasSelection() {
			return false
		}
		return true
	case tea.MouseMsg:
		// Skip messages update if mouse is in the sidebar area
		if m.sidebar != nil && m.sidebarVisible && m.sidebar.IsMouseInSidebar(msg) {
			return true
		}
		return false
	default:
		return false
	}
}

// initialStatsCmd fetches initial agent statistics from daemon
func (m *Model) initialStatsCmd() tea.Cmd {
	return func() tea.Msg {
		// Query daemon for agent list
		req := struct {
			Type string `json:"type"`
		}{Type: "list"}
		b, _ := json.Marshal(req)

		sock := filepath.Join(os.TempDir(), "opperator.sock")
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return statsCountsMsg{err: err}
		}
		defer conn.Close()

		if _, err := conn.Write(append(b, '\n')); err != nil {
			return statsCountsMsg{err: err}
		}

		scanner := bufio.NewScanner(conn)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return statsCountsMsg{err: err}
			}
			return statsCountsMsg{err: fmt.Errorf("no response from daemon")}
		}

		var resp struct {
			Success   bool   `json:"success"`
			Error     string `json:"error"`
			Processes []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"processes"`
		}

		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			return statsCountsMsg{err: err}
		}

		if !resp.Success {
			return statsCountsMsg{err: fmt.Errorf("%s", resp.Error)}
		}

		// Build statuses map
		statuses := make(map[string]string)
		var running, stopped, crashed int
		for _, p := range resp.Processes {
			statuses[p.Name] = p.Status
			switch strings.ToLower(p.Status) {
			case "running":
				running++
			case "crashed":
				crashed++
			default:
				stopped++
			}
		}
		total := len(resp.Processes)
		return initialStatsMsg{statuses: statuses, running: running, stopped: stopped, crashed: crashed, total: total}
	}
}

// updateAgentStatusAndRefreshStats updates local agent status and refreshes stats display
func (m *Model) updateAgentStatusAndRefreshStats(agentName, status string) {
	if m.agentStatuses == nil {
		m.agentStatuses = make(map[string]string)
	}

	m.agentStatuses[agentName] = status

	// Recalculate counts from the map
	var running, stopped, crashed int
	for _, st := range m.agentStatuses {
		switch strings.ToLower(st) {
		case "running":
			running++
		case "crashed":
			crashed++
		default: // stopped or any other status
			stopped++
		}
	}
	total := len(m.agentStatuses)

	if m.stats != nil {
		m.stats.SetProcessCounts(running, stopped, crashed, total)
		m.stats.SetError(nil)
	}
}

// refreshAgentListInSidebar updates the agent list in the sidebar with current statuses from m.agentStatuses
func (m *Model) refreshAgentListInSidebar() {
	if m.sidebar == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	agents, err := llm.ListAgents(ctx)
	if err != nil || len(agents) == 0 {
		return
	}

	// Build agent list with current statuses from m.agentStatuses map
	agentList := make([]cmpsidebar.AgentListItem, 0, len(agents))
	for _, agent := range agents {
		// Use status from our local map if available, otherwise use status from list
		status := agent.Status
		if localStatus, exists := m.agentStatuses[agent.Name]; exists {
			status = localStatus
		}

		agentList = append(agentList, cmpsidebar.AgentListItem{
			Name:        agent.Name,
			Description: agent.Description,
			Status:      status,
			Color:       agent.Color,
		})
	}

	m.sidebar.SetAgentList(agentList)
}
