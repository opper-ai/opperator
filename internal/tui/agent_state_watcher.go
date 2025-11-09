package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"

	"opperator/config"
	cmpsidebar "tui/components/sidebar"
	"tui/coreagent"
	"tui/internal/protocol"
	llm "tui/llm"
	"tui/tools"
	tooling "tui/tools"
	"tui/util"
)

// agentStateEventMsg wraps an agent state change event
type agentStateEventMsg struct {
	AgentName           string
	Type                string
	Description         string
	SystemPrompt        string
	SystemPromptReplace bool
	Color               string
	Logs                []string // For bulk log updates (initial load)
	LogEntry            string   // For single log append events
	CustomSections      []cmpsidebar.CustomSection
	Status              string
	Commands            []protocol.CommandDescriptor
	Daemon              string // NEW: Which daemon this event came from
}

// initAgentStateWatcher initializes the multi-daemon agent state watcher
func (m *Model) initAgentStateWatcher() {
	if m.agentStateCh != nil {
		// Already initialized
		return
	}

	// Shared event channel for all daemons
	eventCh := make(chan agentStateEventMsg, 100) // Buffered to handle multiple daemons
	ctx, cancel := context.WithCancel(context.Background())

	m.agentStateCh = eventCh
	m.agentStateCancel = cancel

	// Load daemon registry
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		// Fallback to local daemon only
		go m.watchSingleDaemon(ctx, "local", eventCh)
		return
	}

	// Start watcher goroutine for each enabled daemon with health check
	for _, daemon := range registry.Daemons {
		if !daemon.Enabled {
			continue
		}

		// For non-local daemons, do a quick health check first
		if daemon.Name != "local" {
			go m.watchWithHealthCheck(ctx, daemon.Name, eventCh)
		} else {
			// Local daemon is always assumed healthy
			go m.watchSingleDaemon(ctx, daemon.Name, eventCh)
		}
	}
}

// watchWithHealthCheck performs a quick health check before starting the watcher
func (m *Model) watchWithHealthCheck(ctx context.Context, daemonName string, eventCh chan<- agentStateEventMsg) {
	// Quick health check with short timeout
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Try to establish connection quickly
	_, cleanup, err := tools.OpenStreamToDaemon(healthCtx, daemonName, struct {
		Type string `json:"type"`
	}{Type: "watch_agent_state"})

	if err != nil {
		// Health check failed - auto-disable daemon
		if disableErr := m.autoDisableDaemon(daemonName); disableErr == nil {
			// Send a special event to notify the TUI
			select {
			case eventCh <- agentStateEventMsg{
				Type:   "daemon_health",
				Daemon: daemonName,
				Status: "disabled",
			}:
			case <-ctx.Done():
			}
		}
		return
	}

	// Health check passed - cleanup and start regular watcher
	cleanup()
	m.watchSingleDaemon(ctx, daemonName, eventCh)
}

// autoDisableDaemon automatically disables an unreachable daemon
func (m *Model) autoDisableDaemon(daemonName string) error {
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return err
	}

	daemon, err := registry.GetDaemon(daemonName)
	if err != nil {
		return err
	}

	daemon.Enabled = false
	if err := registry.AddDaemon(*daemon); err != nil {
		return err
	}

	return config.SaveDaemonRegistry(registry)
}

// watchSingleDaemon watches agent state events from a single daemon
func (m *Model) watchSingleDaemon(ctx context.Context, daemonName string, eventCh chan<- agentStateEventMsg) {
	payload := struct {
		Type string `json:"type"`
	}{Type: "watch_agent_state"}

	conn, cleanup, err := tools.OpenStreamToDaemon(ctx, daemonName, payload)
	if err != nil {
		// Silently fail - daemon may be offline
		return
	}
	defer cleanup()

	// Read initial success response
	scanner := bufio.NewScanner(conn)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 64*1024*1024)

	if !scanner.Scan() {
		return
	}

	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil || !resp.Success {
		return
	}

	// Read events and send to shared channel
	for scanner.Scan() {
		var event struct {
			Type                string                       `json:"type"`
			AgentName           string                       `json:"agent_name"`
			Description         string                       `json:"description,omitempty"`
			SystemPrompt        string                       `json:"system_prompt,omitempty"`
			SystemPromptReplace bool                         `json:"system_prompt_replace,omitempty"`
			Color               string                       `json:"color,omitempty"`
			Logs                []string                     `json:"logs,omitempty"`
			LogEntry            string                       `json:"log_entry,omitempty"`
			CustomSections      interface{}                  `json:"custom_sections,omitempty"`
			Status              string                       `json:"status,omitempty"`
			Commands            []protocol.CommandDescriptor `json:"commands,omitempty"`
		}

		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		// Convert custom sections
		var sections []cmpsidebar.CustomSection
		if event.CustomSections != nil {
			if sectionsData, ok := event.CustomSections.([]interface{}); ok {
				for _, s := range sectionsData {
					if sMap, ok := s.(map[string]interface{}); ok {
						section := cmpsidebar.CustomSection{}
						if id, ok := sMap["id"].(string); ok {
							section.ID = strings.TrimSpace(id)
						}
						if title, ok := sMap["title"].(string); ok {
							section.Title = strings.TrimSpace(title)
						}
						if content, ok := sMap["content"].(string); ok {
							// Trim leading/trailing whitespace to prevent alignment issues
							section.Content = strings.TrimSpace(content)
						}
						if collapsed, ok := sMap["collapsed"].(bool); ok {
							section.Collapsed = collapsed
						}
						sections = append(sections, section)
					}
				}
			}
		}

		select {
		case eventCh <- agentStateEventMsg{
			AgentName:           event.AgentName,
			Type:                event.Type,
			Description:         event.Description,
			SystemPrompt:        event.SystemPrompt,
			SystemPromptReplace: event.SystemPromptReplace,
			Color:               event.Color,
			Logs:                event.Logs,
			LogEntry:            event.LogEntry,
			CustomSections:      sections,
			Status:              event.Status,
			Commands:            protocol.NormalizeCommandDescriptors(event.Commands),
			Daemon:              daemonName, // Tag event with daemon name
		}:
		case <-ctx.Done():
			return
		}
	}
}

// handleFocusAgentEvent handles when the focused agent changes
func (m *Model) handleFocusAgentEvent(msg focusAgentEventMsg) tea.Cmd {
	var cmds []tea.Cmd

	agentName := strings.TrimSpace(msg.Event.Payload.AgentName)

	// Update the global focused agent for tools that need it (e.g., todo tool)
	tooling.SetCurrentFocusedAgent(agentName)

	// Update the agent controller so Builder inherits the focused agent's commands
	if m.agents != nil {
		if agentName == "" {
			m.agents.clearFocusedAgent()
		} else {
			m.agents.setFocusedAgent(agentName)
		}
	}

	// The focused agent is a name reference for Builder to know which agent to work with
	if m.sidebar != nil {
		m.sidebar.SetFocusedAgent(agentName)
		// Set initial status from the agentStatuses map if available
		if agentName != "" {
			if status, ok := m.findAgentStatus(agentName); ok {
				m.sidebar.SetFocusedAgentStatus(status)
			} else {
				m.sidebar.SetFocusedAgentStatus("")
			}
		} else {
			m.sidebar.SetFocusedAgentStatus("")
		}
		m.sidebar.SetFocusedAgentCommands(nil)
		m.sidebar.SetAgentLogs(nil)
		m.sidebar.SetFocusedAgentDescription("")
		// Clear custom sections from the previous focused agent
		// New agent's sections will be fetched via fetchFocusedAgentMetadataCmd
		m.sidebar.SetCustomSections(nil)
	}

	// Persist the focused agent to the conversation
	if m.convStore != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			_ = m.convStore.UpdateFocusedAgent(ctx, m.sessionID, agentName)
		}()
	}

	if cmd := m.refreshSidebar(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if agentName != "" {
		// Try to fetch metadata for agents that are already running
		// This will fail silently for stopped agents, but will populate data for running ones
		cmds = append(cmds, m.fetchFocusedAgentMetadataCmd(agentName))
	}

	cmds = append(cmds, m.waitFocusAgentEvent())

	return tea.Batch(cmds...)
}

// handlePlanEvent handles plan updates for the focused agent
func (m *Model) handlePlanEvent(msg planEventMsg) tea.Cmd {
	// Update sidebar with plan items for the focused agent
	if m.sidebar != nil && m.currentCoreAgentID() == coreagent.IDBuilder {
		focusedName := m.sidebar.FocusedAgentName()
		if msg.Event.Payload.AgentName == focusedName && focusedName != "" {
			// Convert tooling.PlanItem to sidebar.TodoItem (keeping name for now for UI compatibility)
			todos := make([]cmpsidebar.TodoItem, len(msg.Event.Payload.Items))
			for i, item := range msg.Event.Payload.Items {
				todos[i] = cmpsidebar.TodoItem{
					ID:        item.ID,
					Text:      item.Text,
					Completed: item.Completed,
				}
			}
			m.sidebar.SetTodos(todos)
			if cmd := m.refreshSidebar(); cmd != nil {
				return tea.Batch(cmd, m.waitPlanEvent())
			}
		}
	}

	// Wait for the next plan event
	return m.waitPlanEvent()
}

// fetchFocusedAgentMetadataCmd fetches metadata for the focused agent
func (m *Model) fetchFocusedAgentMetadataCmd(agentName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		meta, err := llm.FetchAgentMetadata(ctx, agentName)
		if err != nil {
			return focusedAgentMetadataMsg{
				agentName: agentName,
				err:       err,
			}
		}

		// Logs are now updated via events only (no polling)

		return focusedAgentMetadataMsg{
			agentName: agentName,
			metadata:  meta,
			logs:      nil,
			err:       nil,
		}
	}
}

// handleFocusedAgentMetadata handles the result of fetching focused agent metadata
func (m *Model) handleFocusedAgentMetadata(msg focusedAgentMetadataMsg) tea.Cmd {
	if m.sidebar == nil || m.currentCoreAgentID() != coreagent.IDBuilder {
		return nil
	}

	currentFocused := m.sidebar.FocusedAgentName()
	if currentFocused != msg.agentName {
		// Focused agent changed, ignore stale metadata
		return nil
	}

	if msg.err != nil {
		// Failed to fetch metadata - this shouldn't happen anymore since
		// FetchAgentMetadata returns partial results, but keep as fallback
		m.sidebar.SetFocusedAgentCommands(nil)
		m.sidebar.SetFocusedAgentDescription("")
		// Logs are handled by events, so we don't clear them here
		return nil
	}

	// Set description and color even if commands are empty (agent might be stopped)
	m.sidebar.SetFocusedAgentDescription(msg.metadata.Description)
	m.sidebar.SetFocusedAgentColor(msg.metadata.Color)
	m.sidebar.SetFocusedAgentCommands(msg.metadata.Commands)

	// Fetch initial logs, plan items, and custom sections once when focused agent metadata is loaded
	// Subsequent updates come via events
	return tea.Batch(
		m.fetchInitialAgentLogsCmd(msg.agentName),
		m.fetchInitialPlanItemsCmd(msg.agentName),
		m.fetchInitialCustomSectionsCmd(msg.agentName),
	)
}

// handleAgentMetadataFetched handles async metadata fetch completion for active agent switching
func (m *Model) handleAgentMetadataFetched(msg agentMetadataFetchedMsg) tea.Cmd {
	if m.agents == nil {
		return nil
	}

	// Check if we're still trying to switch to this agent
	currentActive := strings.TrimSpace(m.agents.activeName)
	if currentActive != msg.agentName {
		// Active agent changed, ignore stale metadata
		return nil
	}

	if msg.err != nil {
		// Failed to fetch metadata - clear the pending state and show error
		m.agents.clearActiveAgent()
		return util.ReportError(fmt.Errorf("fetch agent %s: %w", msg.agentName, msg.err))
	}

	// Apply the full metadata
	m.agents.applyActiveAgent(msg.metadata, true)

	// Warn if agent has no commands
	if len(msg.metadata.Commands) == 0 {
		return util.ReportWarn(fmt.Sprintf("Agent %s exposes no commands.", msg.metadata.Name))
	}

	return nil
}

// fetchInitialAgentLogsCmd fetches initial logs for an agent
func (m *Model) fetchInitialAgentLogsCmd(agentName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		logs, err := llm.FetchAgentLogs(ctx, agentName, 50)
		return initialAgentLogsMsg{
			agentName: agentName,
			logs:      logs,
			err:       err,
		}
	}
}

// fetchInitialPlanItemsCmd fetches initial plan items for an agent
func (m *Model) fetchInitialPlanItemsCmd(agentName string) tea.Cmd {
	return func() tea.Msg {
		if m.planStore == nil {
			return initialPlanItemsMsg{
				agentName: agentName,
				items:     nil,
				err:       fmt.Errorf("plan store not initialized"),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		planData, err := m.planStore.GetPlan(ctx, m.sessionID, agentName)
		if err != nil {
			return initialPlanItemsMsg{
				agentName: agentName,
				items:     nil,
				err:       err,
			}
		}

		return initialPlanItemsMsg{
			agentName: agentName,
			items:     planData.Items,
			err:       nil,
		}
	}
}

// fetchInitialCustomSectionsCmd fetches initial custom sections for an agent
func (m *Model) fetchInitialCustomSectionsCmd(agentName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		sections, err := llm.FetchAgentCustomSections(ctx, agentName)
		return initialCustomSectionsMsg{
			agentName: agentName,
			sections:  sections,
			err:       err,
		}
	}
}

// handleInitialAgentLogs handles the initial agent logs response
func (m *Model) handleInitialAgentLogs(msg initialAgentLogsMsg) tea.Cmd {
	if m.sidebar == nil {
		return nil
	}

	// Only set logs if this is still the active agent or focused agent
	currentActive := m.currentActiveAgentName()
	coreID := m.currentCoreAgentID()

	shouldSetLogs := false
	if coreID == coreagent.IDBuilder {
		// In Builder mode, check if this is the focused agent OR active agent
		// (active agent can run without being explicitly focused)
		focusedName := m.sidebar.FocusedAgentName()

		// Show logs if this is the currently active agent, OR if it's the focused agent
		if currentActive == msg.agentName || (focusedName != "" && focusedName == msg.agentName) {
			shouldSetLogs = true
		}
	} else {
		// In other modes, check if this is the current active agent
		if currentActive == msg.agentName {
			shouldSetLogs = true
		}
	}

	if shouldSetLogs {
		if msg.err == nil && msg.logs != nil && len(msg.logs) > 0 {
			m.sidebar.SetAgentLogs(msg.logs)
		} else if msg.err != nil {
			// Show error in logs section
			m.sidebar.SetAgentLogs([]string{
				fmt.Sprintf("[%s] Error fetching logs: %v", time.Now().Format("15:04:05"), msg.err),
			})
		} else {
			// No logs available yet - show helpful message
			m.sidebar.SetAgentLogs([]string{
				fmt.Sprintf("[%s] Agent '%s' has no logs yet", time.Now().Format("15:04:05"), msg.agentName),
				"Logs will appear here when the agent starts running.",
			})
		}
	}

	return nil
}

// handleInitialPlanItems handles the initial plan items response
func (m *Model) handleInitialPlanItems(msg initialPlanItemsMsg) tea.Cmd {
	if m.sidebar == nil || m.currentCoreAgentID() != coreagent.IDBuilder {
		return nil
	}

	// Only set plan items if this is still the focused agent
	focusedName := m.sidebar.FocusedAgentName()
	if focusedName != msg.agentName || focusedName == "" {
		// Focused agent changed, ignore stale plan items
		return nil
	}

	if msg.err == nil && msg.items != nil {
		// Convert plan.PlanItem to sidebar.TodoItem
		todos := make([]cmpsidebar.TodoItem, len(msg.items))
		for i, item := range msg.items {
			todos[i] = cmpsidebar.TodoItem{
				ID:        item.ID,
				Text:      item.Text,
				Completed: item.Completed,
			}
		}
		m.sidebar.SetTodos(todos)
		return m.refreshSidebar()
	}

	return nil
}

// handleInitialCustomSections handles the initial custom sections response
func (m *Model) handleInitialCustomSections(msg initialCustomSectionsMsg) tea.Cmd {
	if m.sidebar == nil {
		return nil
	}

	// Only set custom sections if this is still the active agent or focused agent
	currentActive := m.currentActiveAgentName()
	coreID := m.currentCoreAgentID()

	shouldSetSections := false
	if coreID == coreagent.IDBuilder {
		// In Builder mode, check if this is the focused agent OR active agent
		// (active agent can run without being explicitly focused)
		focusedName := m.sidebar.FocusedAgentName()

		// Show custom sections if this is the currently active agent, OR if it's the focused agent
		if currentActive == msg.agentName || (focusedName != "" && focusedName == msg.agentName) {
			shouldSetSections = true
		}
	} else {
		// In other modes, check if this is the current active agent
		if currentActive == msg.agentName {
			shouldSetSections = true
		}
	}

	if shouldSetSections && msg.err == nil && msg.sections != nil {
		m.sidebar.SetCustomSections(msg.sections)
		return m.refreshSidebar()
	}

	return nil
}
