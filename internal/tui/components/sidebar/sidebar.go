package sidebar

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"tui/internal/protocol"
	"tui/styles"
)

type Sidebar struct {
	w, h int

	// State components
	agent    *AgentState
	builder  *BuilderState
	sections *SectionState
	logs     *ViewportState
	nav      *NavigationState

	// Custom section viewports (keyed by section ID)
	customViewports map[string]*CustomViewportState

	// Navigation helper
	navHelper *NavigationHelper

	// Mouse handler
	mouseHandler *MouseHandler

	// UI state
	focused bool

	prefsStore PreferencesStore
}

func New(prefsStore PreferencesStore) *Sidebar {
	agent := NewAgentState()
	builder := NewBuilderState()
	sections := NewSectionState()
	logs := NewViewportState()
	nav := NewNavigationState()

	s := &Sidebar{
		prefsStore:      prefsStore,
		agent:           agent,
		builder:         builder,
		sections:        sections,
		logs:            logs,
		nav:             nav,
		customViewports: make(map[string]*CustomViewportState),
	}

	// Create navigation helper
	s.navHelper = NewNavigationHelper(nav, agent, builder, sections)

	// Create mouse handler (will be updated with position in SetPosition)
	s.mouseHandler = NewMouseHandler(0, 0, logs, sections)

	// Load preferences for sections
	s.sections.LoadPreferences(prefsStore)

	return s
}

func (s *Sidebar) initLogsViewport() {
	s.logs.Init()
}

func (s *Sidebar) initCustomViewport(sectionID string) {
	if _, exists := s.customViewports[sectionID]; !exists {
		s.customViewports[sectionID] = NewCustomViewportState()
	}
	s.customViewports[sectionID].Init()
}

func (s *Sidebar) SetSize(w, h int) {
	if s.w != w || s.h != h {
		s.w, s.h = w, h
		s.mouseHandler.UpdatePosition(s.mouseHandler.sidebarX, w)
	}
}

func (s *Sidebar) SetPosition(x int) {
	s.mouseHandler.UpdatePosition(x, s.w)
}

func (s *Sidebar) IsMouseInSidebar(msg tea.Msg) bool {
	return s.mouseHandler.IsMouseInSidebar(msg)
}

func (s *Sidebar) SetAgentInfo(agentName, agentDescription, agentColor string, commands []protocol.CommandDescriptor) (changed bool, agentNameChanged bool) {
	changed, agentNameChanged = s.agent.SetInfo(agentName, agentDescription, agentColor, commands)
	if changed {
		s.validateAndFixSelection()
	}
	return changed, agentNameChanged
}

func (s *Sidebar) SetAgentLogs(logs []string) {
	changed := s.agent.SetLogs(logs)
	if !changed {
		return
	}

	// If viewport is at bottom or not initialized yet, keep auto-scroll enabled
	if s.logs.IsAtBottom() {
		s.logs.AutoScroll = true
	}
}

func (s *Sidebar) AppendAgentLog(logEntry string) {
	// If viewport is at bottom, keep auto-scroll enabled
	if s.logs.IsAtBottom() {
		s.logs.AutoScroll = true
	}

	s.agent.AppendLog(logEntry)
}

func (s *Sidebar) SetAgentList(agents []AgentListItem) {
	changed := s.agent.SetList(agents)
	if changed {
		s.validateAndFixSelection()
	}
}

func (s *Sidebar) HasAgentList() bool {
	return len(s.agent.List) > 0
}

func (s *Sidebar) SetFocusedAgent(agentName string) {
	changed := s.builder.SetFocusedAgent(agentName)
	if changed {
		s.validateAndFixSelection()
	}
}

func (s *Sidebar) FocusedAgentName() string {
	return s.builder.FocusedAgentName
}

func (s *Sidebar) FocusedAgentDescription() string {
	return s.builder.FocusedAgentDescription
}

func (s *Sidebar) FocusedAgentTodos() []TodoItem {
	return s.builder.Todos
}

func (s *Sidebar) SetFocusedAgentStatus(status string) {
	s.builder.SetFocusedAgentStatus(status)
}

func (s *Sidebar) SetFocusedAgentDescription(description string) {
	s.builder.SetFocusedAgentDescription(description)
}

func (s *Sidebar) SetFocusedAgentColor(color string) {
	s.builder.SetFocusedAgentColor(color)
}

func (s *Sidebar) SetFocusedAgentCommands(commands []protocol.CommandDescriptor) {
	s.builder.SetFocusedAgentCommands(commands)
}

func (s *Sidebar) SetTodos(todos []TodoItem) {
	s.builder.SetTodos(todos)
}

func (s *Sidebar) SetCustomSections(sections []CustomSection) {
	changed := s.sections.SetCustomSections(sections, s.prefsStore)
	if changed {
		// Clean up viewports for removed sections
		currentSectionIDs := make(map[string]struct{}, len(sections))
		for i := range sections {
			currentSectionIDs[sections[i].ID] = struct{}{}
		}
		for sectionID := range s.customViewports {
			if _, exists := currentSectionIDs[sectionID]; !exists {
				delete(s.customViewports, sectionID)
			}
		}
		s.validateAndFixSelection()
	}
}

func (s *Sidebar) Focus() {
	if !s.focused {
		s.focused = true
	}
}

func (s *Sidebar) Blur() {
	if s.focused {
		s.focused = false
	}
}

func (s *Sidebar) HasFocus() bool {
	return s.focused
}

func (s *Sidebar) Update(msg tea.Msg) tea.Cmd {
	// Initialize viewport if needed when logs section is expanded
	if s.sections.LogsExpanded && !s.logs.Inited {
		s.initLogsViewport()
	}

	// Initialize viewports for expanded custom sections
	for _, section := range s.sections.CustomSections {
		if s.sections.CustomSectionsExpanded[section.ID] {
			vp, exists := s.customViewports[section.ID]
			if !exists || !vp.Inited {
				s.initCustomViewport(section.ID)
			}
		}
	}

	// Handle mouse events for logs viewport
	logsCmd := s.mouseHandler.HandleLogsViewportMouse(msg)

	// Handle mouse events for custom section viewports
	customCmd := s.mouseHandler.HandleCustomSectionViewportsMouse(msg, s.customViewports)

	// Return the first non-nil command
	if logsCmd != nil {
		return logsCmd
	}
	return customCmd
}

func (s *Sidebar) FocusNext() bool {
	return s.navHelper.FocusNext()
}

func (s *Sidebar) FocusPrev() bool {
	return s.navHelper.FocusPrev()
}

func (s *Sidebar) ToggleSection() {
	selectedCustomIdx := s.nav.GetSelectedCustomIndex()
	if selectedCustomIdx >= 0 && selectedCustomIdx < len(s.sections.CustomSections) {
		section := &s.sections.CustomSections[selectedCustomIdx]
		s.sections.ToggleCustomSection(section.ID, s.prefsStore)
		return
	}

	selectedSection := s.nav.GetSelectedSection()
	switch selectedSection {
	case SectionAgentInfo:
		s.sections.ToggleAgentInfo(s.prefsStore)
	case SectionAgents:
		s.sections.ToggleAgents(s.prefsStore)
	case SectionFocusedAgent:
		s.sections.ToggleFocusedAgentInfo(s.prefsStore)
	case SectionTodos:
		s.sections.ToggleTodos(s.prefsStore)
	case SectionTools:
		s.sections.ToggleTools(s.prefsStore)
	case SectionLogs:
		s.sections.ToggleLogs(s.prefsStore)
	}
}

func (s *Sidebar) getTotalSectionCount() int {
	return s.navHelper.GetTotalSectionCount()
}

func (s *Sidebar) getAvailableSections() []Section {
	return s.navHelper.GetAvailableSections()
}

// findSectionIndex finds the index of a section in the available sections list
func (s *Sidebar) findSectionIndex(sections []Section, target Section) int {
	return s.navHelper.FindSectionIndex(sections, target)
}

// validateAndFixSelection ensures the current selection is valid for available sections
func (s *Sidebar) validateAndFixSelection() {
	s.navHelper.ValidateAndFixSelection()
}

func (s *Sidebar) View() string {
	if s.w <= 0 || s.h <= 0 {
		return ""
	}

	t := styles.CurrentTheme()

	borderColor := t.Border
	if s.focused {
		borderColor = lipgloss.Color("#8a6a60")
	}

	box := lipgloss.NewStyle().
		Width(s.w).
		Height(max(s.h, 1)).
		MarginLeft(2).
		PaddingLeft(1).
		PaddingRight(1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		BorderTop(false).
		BorderRight(false).
		BorderBottom(false).
		BorderLeft(true)

	state := NewSidebarRenderState(t)

	s.renderAgentInfoSection(&state)
	s.renderAgentsSection(&state)
	s.renderBuilderTodosSection(&state)
	s.renderBuilderDivider(&state)
	s.renderFocusedAgentSection(&state)

	s.appendTitleLine(&state)
	s.appendBuilderIntroLines(&state)

	tools, slashCommands := s.commandsForView()
	s.renderCommandsSection(&state, tools, slashCommands)
	s.renderLogsSection(&state)
	s.renderCustomSections(&state)
	s.appendToggleHint(&state)

	return state.Render(box)
}

func (s *Sidebar) renderAgentInfoSection(state *SidebarRenderState) {
	if s.agent.Name == "" {
		return
	}

	t := state.Theme
	isSelected := s.focused && s.nav.GetSelectedSection() == SectionAgentInfo
	boxWithLabel := NewBoxWithLabel(t, isSelected)

	indicator := "+"
	if s.sections.AgentInfoExpanded {
		indicator = "-"
	}

	var labelStyle lipgloss.Style
	switch s.agent.Name {
	case "Opperator":
		labelStyle = lipgloss.NewStyle().Foreground(t.Primary).Bold(true)
	case "Builder":
		labelStyle = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	default:
		if s.agent.Color != "" {
			labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(s.agent.Color)).Bold(true)
		} else {
			labelStyle = t.S().Title
		}
	}

	label := t.S().Base.Foreground(t.FgSubtle).Render(indicator) + " " + labelStyle.Render(s.agent.Name)

	var content string
	if s.agent.Description != "" {
		if s.sections.AgentInfoExpanded {
			content = t.S().Base.Foreground(t.FgSubtle).Render(s.agent.Description)
		} else {
			desc := s.agent.Description
			maxLen := 50
			if len(desc) > maxLen {
				desc = desc[:maxLen-3] + "..."
			}
			content = t.S().Base.Foreground(t.FgMuted).Render(desc)
		}
	}

	section := boxWithLabel.Render(label, content, s.sectionWidth())
	state.AddSection(section)
}

func (s *Sidebar) renderAgentsSection(state *SidebarRenderState) {
	if s.agent.Name != "Opperator" || len(s.agent.List) == 0 {
		return
	}

	t := state.Theme
	isSelected := s.focused && s.nav.GetSelectedSection() == SectionAgents
	boxWithLabel := NewBoxWithLabel(t, isSelected)

	indicator := "+"
	if s.sections.AgentsExpanded {
		indicator = "-"
	}

	label := t.S().Base.Foreground(t.FgSubtle).Render(indicator) + " " + t.S().Base.Bold(true).Render("Agents")

	var content string
	if s.sections.AgentsExpanded {
		var agentLines []string
		for _, agent := range s.agent.List {
			var statusStyle lipgloss.Style
			switch strings.ToLower(agent.Status) {
			case "active", "running":
				statusStyle = lipgloss.NewStyle().Foreground(t.Success)
			case "inactive", "idle":
				statusStyle = lipgloss.NewStyle().Foreground(t.FgMuted)
			case "crashed":
				statusStyle = lipgloss.NewStyle().Foreground(t.Error)
			case "error", "failed":
				statusStyle = lipgloss.NewStyle().Foreground(t.Error)
			default:
				statusStyle = lipgloss.NewStyle().Foreground(t.FgSubtle)
			}

			// Use agent's custom color if available, otherwise use default Accent
			nameStyle := t.S().Base.Foreground(t.Accent).Bold(true)
			if agent.Color != "" {
				nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(agent.Color)).Bold(true)
			}

			agentLine := statusStyle.Render("●") + " " + nameStyle.Render(agent.Name)

			if agent.Description != "" {
				agentLine += " " + t.S().Base.Foreground(t.FgSubtle).Render("- "+agent.Description)
			}

			// Add daemon tag if agent is on a remote daemon
			if daemon := strings.TrimSpace(agent.Daemon); daemon != "" && daemon != "local" {
				daemonTag := t.S().Base.Foreground(t.FgSubtle).Render(" [" + daemon + "]")
				agentLine += daemonTag
			}

			agentLines = append(agentLines, agentLine)
		}
		content = lipgloss.JoinVertical(lipgloss.Left, agentLines...)
	} else {
		count := lipgloss.NewStyle().Foreground(t.FgSubtle).Render(fmt.Sprintf("%d", len(s.agent.List)))
		content = count + t.S().Base.Foreground(t.FgMuted).Render(" available agents")
	}

	state.AddSection(boxWithLabel.Render(label, content, s.sectionWidth()))
}

func (s *Sidebar) renderBuilderTodosSection(state *SidebarRenderState) {
	if s.agent.Name != "Builder" || s.builder.FocusedAgentName == "" {
		return
	}

	t := state.Theme
	isSelected := s.focused && s.nav.GetSelectedSection() == SectionTodos
	boxWithLabel := NewBoxWithLabel(t, isSelected)

	indicator := "+"
	if s.sections.TodosExpanded {
		indicator = "-"
	}

	label := t.S().Base.Foreground(t.FgSubtle).Render(indicator) + " " + t.S().Base.Bold(true).Render("Todos")

	var content string
	if len(s.builder.Todos) > 0 {
		if s.sections.TodosExpanded {
			var todoLines []string
			for _, todo := range s.builder.Todos {
				var todoLine string
				if todo.Completed {
					todoLine = lipgloss.NewStyle().Foreground(t.Success).Render("✓") + " " +
						t.S().Base.Foreground(t.FgMuted).Strikethrough(true).Render(todo.Text)
				} else {
					todoLine = t.S().Base.Foreground(t.FgSubtle).Render("☐") + " " +
						t.S().Base.Foreground(t.FgBase).Render(todo.Text)
				}
				todoLines = append(todoLines, todoLine)
			}
			content = lipgloss.JoinVertical(lipgloss.Left, todoLines...)
		} else {
			completed := 0
			for _, todo := range s.builder.Todos {
				if todo.Completed {
					completed++
				}
			}
			total := len(s.builder.Todos)
			count := lipgloss.NewStyle().Foreground(t.FgSubtle).Render(fmt.Sprintf("%d/%d", completed, total))
			content = count + t.S().Base.Foreground(t.FgMuted).Render(" tasks completed")
		}
	} else {
		content = t.S().Base.Foreground(t.FgMuted).Italic(true).Render("No todos yet")
	}

	state.AddSection(boxWithLabel.Render(label, content, s.sectionWidth()))
}

func (s *Sidebar) renderBuilderDivider(state *SidebarRenderState) {
	if s.agent.Name != "Builder" || s.builder.FocusedAgentName == "" {
		return
	}

	repeatCount := max(s.w-8, 0)
	dividerLine := strings.Repeat("─", repeatCount)
	dividerView := lipgloss.NewStyle().
		Foreground(state.Theme.FgMuted).
		Faint(true).
		MarginLeft(2).
		MarginRight(1).
		MarginTop(1).
		MarginBottom(1).
		Render(dividerLine)

	state.AddSection(dividerView)
}

func (s *Sidebar) renderFocusedAgentSection(state *SidebarRenderState) {
	if s.agent.Name != "Builder" || s.builder.FocusedAgentName == "" {
		return
	}

	t := state.Theme
	isSelected := s.focused && s.nav.GetSelectedSection() == SectionFocusedAgent
	boxWithLabel := NewBoxWithLabel(t, isSelected)

	indicator := "+"
	if s.sections.FocusedAgentInfoExpanded {
		indicator = "-"
	}

	// Use agent's custom color if available, otherwise use default Accent
	labelStyle := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	if s.builder.FocusedAgentColor != "" {
		labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(s.builder.FocusedAgentColor)).Bold(true)
	}
	label := t.S().Base.Foreground(t.FgSubtle).Render(indicator) + " " + labelStyle.Render(s.builder.FocusedAgentName)

	if s.builder.FocusedAgentStatus != "" {
		separator := lipgloss.NewStyle().Foreground(t.FgMuted).Render(" - ")
		var statusView string
		switch strings.ToLower(s.builder.FocusedAgentStatus) {
		case "running":
			statusView = lipgloss.NewStyle().Foreground(t.Success).Render(s.builder.FocusedAgentStatus)
		case "crashed":
			statusView = lipgloss.NewStyle().Foreground(t.Error).Render(s.builder.FocusedAgentStatus)
		case "stopped":
			statusView = lipgloss.NewStyle().Foreground(t.FgMuted).Render(s.builder.FocusedAgentStatus)
		default:
			statusView = lipgloss.NewStyle().Foreground(t.FgSubtle).Render(s.builder.FocusedAgentStatus)
		}
		label += separator + statusView
	}

	var content string
	if s.builder.FocusedAgentDescription != "" {
		if s.sections.FocusedAgentInfoExpanded {
			content = t.S().Base.Foreground(t.FgSubtle).Render(s.builder.FocusedAgentDescription)
		} else {
			desc := s.builder.FocusedAgentDescription
			maxLen := 50
			if len(desc) > maxLen {
				desc = desc[:maxLen-3] + "..."
			}
			content = t.S().Base.Foreground(t.FgMuted).Render(desc)
		}
	}

	state.AddSection(boxWithLabel.Render(label, content, s.sectionWidth()))
}

func (s *Sidebar) appendTitleLine(state *SidebarRenderState) {
	if s.agent.Name != "" {
		return
	}
	state.AddLine(state.Theme.S().Title.MarginRight(100).Render("Sidebar"))
}

func (s *Sidebar) appendBuilderIntroLines(state *SidebarRenderState) {
	if s.agent.Name != "Builder" || s.builder.FocusedAgentName != "" {
		return
	}

	t := state.Theme
	state.AddLine("")
	state.AddLine(t.S().Base.Padding(0, 2).Foreground(t.FgSubtle).Render("You are now interacting with the Builder agent."))
	state.AddLine("")
	state.AddLine(t.S().Base.Padding(0, 2).Italic(true).Foreground(t.FgMuted).Render("No agent is currently focused."))
	state.AddLine("")
	state.AddLine(t.S().Base.Padding(0, 2).Foreground(t.FgSubtle).Render("Write your first message to start building an agent\nor ask it to modify the behaviour of an existing one."))
	state.AddLine("")
	state.AddLine(t.S().Base.Padding(0, 2).Foreground(t.FgSubtle).Render("Examples:"))
	state.AddLine("")
	state.AddLine(t.S().Base.Padding(0, 2).Foreground(t.FgMuted).Italic(true).Render("- Build me an agent that can fetch the\n  top hacker news post every day"))
	state.AddLine("")
	state.AddLine(t.S().Base.Padding(0, 2).Foreground(t.FgMuted).Italic(true).Render("- Fix my agent \"email-scanner\", it stopped working"))
	state.AddLine("")
	state.AddLine(t.S().Base.Padding(0, 2).Foreground(t.FgMuted).Italic(true).Render("- Create an agent that monitors GitHub\n  repositories for new issues and PRs"))
	state.AddLine("")
	state.AddLine(t.S().Base.Padding(0, 2).Foreground(t.FgMuted).Italic(true).Render("- Build an agent for running database\n  migrations and backups"))
	state.AddLine("")
	state.AddLine(t.S().Base.Padding(0, 2).Foreground(t.FgMuted).Italic(true).Render("- Make me an agent that can generate\n  code documentation from source files"))
}

func (s *Sidebar) commandsForView() (tools, slash []protocol.CommandDescriptor) {
	commands := s.agent.Commands
	if s.agent.Name == "Builder" && s.builder.FocusedAgentName != "" {
		commands = s.builder.FocusedAgentCommands
	}

	for _, cmd := range commands {
		isSlash := false
		for _, exposure := range cmd.ExposeAs {
			if exposure == protocol.CommandExposureSlashCommand {
				isSlash = true
				break
			}
		}
		if isSlash {
			slash = append(slash, cmd)
		} else {
			tools = append(tools, cmd)
		}
	}

	return
}

func (s *Sidebar) renderCommandsSection(state *SidebarRenderState, tools, slashCommands []protocol.CommandDescriptor) {
	if s.agent.Name == "" {
		return
	}
	if s.agent.Name == "Opperator" {
		return
	}
	if s.agent.Name == "Builder" && s.builder.FocusedAgentName == "" {
		return
	}

	t := state.Theme
	isSelected := s.focused && s.nav.GetSelectedSection() == SectionTools
	boxWithLabel := NewBoxWithLabel(t, isSelected)

	indicator := "+"
	if s.sections.ToolsExpanded {
		indicator = "-"
	}

	label := t.S().Base.Foreground(t.FgSubtle).Render(indicator) + " " + t.S().Base.Bold(true).Render("Commands")

	totalCommands := len(tools) + len(slashCommands)
	var content string

	if totalCommands > 0 {
		if s.sections.ToolsExpanded {
			var commandLines []string

			if len(tools) > 0 {
				for _, cmd := range tools {
					cmdTitle := cmd.Title
					if cmdTitle == "" {
						cmdTitle = cmd.Name
					}
					cmdLine := t.S().Base.Foreground(t.Accent).Render(cmdTitle)
					if cmd.Async {
						cmdLine += t.S().Base.Foreground(t.FgMuted).Render(" (async)")
					}
					if cmd.Description != "" {
						cmdLine += t.S().Base.Foreground(t.FgSubtle).Render(" - " + cmd.Description)
					}
					commandLines = append(commandLines, cmdLine)
				}
			}

			if len(slashCommands) > 0 {
				for _, cmd := range slashCommands {
					cmdLine := t.S().Base.Foreground(t.Accent).Render(cmd.SlashCommand)
					if cmd.Async {
						cmdLine += t.S().Base.Foreground(t.FgMuted).Render(" (async)")
					}
					if cmd.Description != "" {
						cmdLine += t.S().Base.Foreground(t.FgSubtle).Render(" - " + cmd.Description)
					}
					commandLines = append(commandLines, cmdLine)
				}
			}

			content = lipgloss.JoinVertical(lipgloss.Left, commandLines...)
		} else {
			count := lipgloss.NewStyle().Foreground(t.Secondary).Render(fmt.Sprintf("%d", totalCommands))
			var breakdown string
			if len(tools) > 0 && len(slashCommands) > 0 {
				breakdown = fmt.Sprintf(" (%d tools, %d slash)", len(tools), len(slashCommands))
			} else if len(tools) > 0 {
				breakdown = " tools"
			} else {
				breakdown = " slash commands"
			}
			content = count + t.S().Base.Foreground(t.FgMuted).Render(breakdown)
		}
	} else {
		if s.agent.Name == "Builder" && s.builder.FocusedAgentName != "" {
			// Check if agent is stopped/crashed
			focusedStatus := strings.ToLower(strings.TrimSpace(s.builder.FocusedAgentStatus))
			if focusedStatus == "stopped" || focusedStatus == "crashed" || focusedStatus == "" {
				content = t.S().Base.Foreground(t.FgMuted).Italic(true).Render("Start agent to see commands")
			} else {
				content = t.S().Base.Foreground(t.FgMuted).Italic(true).Render("No commands")
			}
		} else {
			content = t.S().Base.Foreground(t.FgMuted).Italic(true).Render("No commands have been registered")
		}
	}

	state.AddSection(boxWithLabel.Render(label, content, s.sectionWidth()))
}

func (s *Sidebar) renderLogsSection(state *SidebarRenderState) {
	if s.agent.Name == "" {
		return
	}
	if s.agent.Name == "Builder" && s.builder.FocusedAgentName == "" {
		return
	}

	t := state.Theme
	isSelected := s.focused && s.nav.GetSelectedSection() == SectionLogs
	boxWithLabel := NewBoxWithLabel(t, isSelected)

	indicator := "+"
	if s.sections.LogsExpanded {
		indicator = "-"
	}

	label := t.S().Base.Foreground(t.FgSubtle).Render(indicator) + " " + t.S().Base.Bold(true).Render("Logs")

	var content string
	if len(s.agent.Logs) > 0 {
		if s.sections.LogsExpanded {
			s.initLogsViewport()

			boxWidth := s.sectionWidth()
			vpWidth := boxWidth - 4
			if vpWidth < 1 {
				vpWidth = 1
			}

			vpHeight := min(len(s.agent.Logs), 20)

			s.logs.SetSize(vpWidth, vpHeight)

			truncatedLogs := make([]string, 0, len(s.agent.Logs))
			for _, log := range s.agent.Logs {
				if len(log) > vpWidth && vpWidth > 3 {
					truncatedLogs = append(truncatedLogs, log[:vpWidth-3]+"...")
				} else {
					truncatedLogs = append(truncatedLogs, log)
				}
			}
			logsContent := strings.Join(truncatedLogs, "\n")
			styledContent := t.S().Base.Foreground(t.FgMuted).Render(logsContent)
			s.logs.SetContent(styledContent)

			// Auto-scroll to bottom if enabled (on new content or initially)
			if s.logs.AutoScroll {
				s.logs.GotoBottom()
			}

			content = s.logs.View()
		} else {
			lastLog := s.agent.Logs[len(s.agent.Logs)-1]
			truncated := truncateToOneLine(lastLog, s.w-8)
			content = t.S().Base.Foreground(t.FgMuted).Render(truncated)
		}
	} else {
		// Check if we're in Builder mode with a focused agent
		if s.agent.Name == "Builder" && s.builder.FocusedAgentName != "" {
			// Check if agent is stopped/crashed
			focusedStatus := strings.ToLower(strings.TrimSpace(s.builder.FocusedAgentStatus))
			if focusedStatus == "stopped" || focusedStatus == "crashed" || focusedStatus == "" {
				content = t.S().Base.Foreground(t.FgMuted).Italic(true).Render("Start agent to see logs")
			} else {
				content = t.S().Base.Foreground(t.FgMuted).Italic(true).Render("No logs")
			}
		} else {
			content = t.S().Base.Foreground(t.FgMuted).Italic(true).Render("No logs")
		}
	}

	s.logs.Y = state.CumulativeHeight

	var logsSection string
	if s.sections.LogsExpanded && len(s.agent.Logs) > 0 {
		vpHeight := min(len(s.agent.Logs), 20)
		logsSection = boxWithLabel.RenderWithScrollbar(label, content, s.sectionWidth(), vpHeight, len(s.agent.Logs), s.logs.YOffset())
	} else {
		logsSection = boxWithLabel.Render(label, content, s.sectionWidth())
	}

	s.logs.Height = lipgloss.Height(logsSection)
	state.AddSection(logsSection)
}

func (s *Sidebar) renderCustomSections(state *SidebarRenderState) {
	if len(s.sections.CustomSections) == 0 {
		return
	}

	t := state.Theme
	for i, section := range s.sections.CustomSections {
		isSelected := s.focused && s.nav.GetSelectedCustomIndex() == i
		boxWithLabel := NewBoxWithLabel(t, isSelected)

		expanded := s.sections.CustomSectionsExpanded[section.ID]
		indicator := "+"
		if expanded {
			indicator = "-"
		}

		label := t.S().Base.Foreground(t.FgSubtle).Render(indicator) + " " + t.S().Base.Bold(true).Render(section.Title)

		defaultStyle := t.S().Base.Foreground(t.FgMuted)
		var content string
		var renderedSection string

		if expanded {
			// Initialize viewport for this section
			s.initCustomViewport(section.ID)
			vp := s.customViewports[section.ID]

			// Set viewport size
			boxWidth := s.sectionWidth()
			vpWidth := boxWidth - 4
			if vpWidth < 1 {
				vpWidth = 1
			}

			// Parse the content with markup styling and manually wrap lines
			rawContent := ParseMarkupWithStyle(section.Content, defaultStyle)

			// Manually wrap each line to fit within viewport width
			contentLines := strings.Split(rawContent, "\n")
			var wrappedLines []string
			for _, line := range contentLines {
				if line == "" {
					wrappedLines = append(wrappedLines, line)
					continue
				}
				// Use lipgloss to wrap the line
				wrapped := lipgloss.NewStyle().Width(vpWidth).Render(line)
				wrappedParts := strings.Split(wrapped, "\n")
				wrappedLines = append(wrappedLines, wrappedParts...)
			}

			styledContent := strings.Join(wrappedLines, "\n")
			totalLines := len(wrappedLines)

			// Set viewport height: minimum of total lines or 20 (same as logs)
			vpHeight := min(totalLines, 20)
			vp.SetSize(vpWidth, vpHeight)
			vp.SetContent(styledContent)

			// Get rendered viewport content
			content = vp.View()

			// Store position for mouse handling
			vp.Y = state.CumulativeHeight

			// Render with scrollbar if content exceeds viewport
			if totalLines > vpHeight {
				renderedSection = boxWithLabel.RenderWithScrollbar(label, content, s.sectionWidth(), vpHeight, totalLines, vp.YOffset())
			} else {
				renderedSection = boxWithLabel.Render(label, content, s.sectionWidth())
			}

			// Update height after rendering
			vp.Height = lipgloss.Height(renderedSection)
		} else {
			// Collapsed: show first line truncated
			firstLine := section.Content
			if idx := strings.IndexAny(firstLine, "\n\r"); idx != -1 {
				firstLine = firstLine[:idx]
			}
			truncated := truncateToOneLine(firstLine, s.w-8)
			content = ParseMarkupWithStyle(truncated, defaultStyle)
			renderedSection = boxWithLabel.Render(label, content, s.sectionWidth())
		}

		state.AddSection(renderedSection)
	}
}

func (s *Sidebar) appendToggleHint(state *SidebarRenderState) {
	if s.agent.Name != "" {
		return
	}

	t := state.Theme
	state.AddLine("")
	state.AddLine(t.S().Base.Foreground(t.FgSubtle).Render("Use ctrl+b to toggle"))
}

func (s *Sidebar) sectionWidth() int {
	return s.w - 4
}
