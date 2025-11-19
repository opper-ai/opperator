package agentlist

import (
	"context"
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"tui/components/textarea"
	"tui/internal/viewport"
	llm "tui/llm"
	"tui/styles"
	tooling "tui/tools"

	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/spinner"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
)

type Model struct {
	width, height int
	keys          KeyMap
	help          help.Model
	spinner       spinner.Model
	viewport      viewport.Model

	agents  []llm.AgentInfo
	cursor  int
	loading bool
	err     error

	// Pending actions: map[agentName]actionName (e.g. "starting", "stopping")
	pending map[string]string

	// Search/Filter
	searchInput *textarea.Model
	filtering   bool
}

type KeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Start   key.Binding
	Stop    key.Binding
	Restart key.Binding
	Close   key.Binding
	Enter   key.Binding
	Escape  key.Binding
	Tab     key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Start: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "start"),
		),
		Stop: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "stop"),
		),
		Restart: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "restart"),
		),
		Close: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch focus"),
		),
	}
}

// ShortHelp returns keybindings to be shown in the mini help view. It's part
// of the help.KeyMap interface.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Start, k.Stop, k.Restart, k.Tab, k.Close}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// help.KeyMap interface.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Start},
		{k.Stop, k.Restart, k.Tab, k.Close},
	}
}

func New() *Model {
	h := help.New()
	t := styles.CurrentTheme()
	h.Styles = t.S().Help

	s := spinner.New()
	s.Spinner = spinner.Spinner{
		Frames: []string{"●", "○"},
		FPS:    time.Second / 2,
	}
	s.Style = lipgloss.NewStyle().Foreground(t.Warning)

	ti := textarea.New()
	ti.Placeholder = "Filter agents... (press tab to focus)"
	ti.Prompt = "┃ "
	ti.SetHeight(1)
	ti.ShowLineNumbers = false
	ti.CharLimit = 100

	// Match input.go styling
	st := t.S().TextArea
	st.Cursor.Blink = true
	st.Cursor.Shape = tea.CursorBar
	ti.SetStyles(st)
	// Do not focus search input by default

	m := &Model{
		keys:        DefaultKeyMap(),
		help:        h,
		spinner:     s,
		viewport:    viewport.New(),
		pending:     make(map[string]string),
		searchInput: ti,
		filtering:   false, // Default to list focus
	}
	return m
}

func (m *Model) IsFiltering() bool {
	return m.filtering
}

func (m *Model) Init() tea.Cmd {
	// Do not focus search input in Init
	return tea.Batch(m.Refresh(), m.spinner.Tick, m.pollUpdates())
}

type pollMsg struct{}

func (m *Model) pollUpdates() tea.Cmd {
	return tea.Tick(1*time.Second, func(_ time.Time) tea.Msg {
		return pollMsg{}
	})
}

func (m *Model) Refresh() tea.Cmd {
	// Don't set loading=true here to avoid flickering during polling
	return func() tea.Msg {
		agents, err := llm.ListAgents(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return agentsMsg{agents}
	}
}

type agentsMsg struct {
	agents []llm.AgentInfo
}

type errMsg struct {
	err error
}

type actionResultMsg struct {
	agentName string
	err       error
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Tab always toggles focus
		if key.Matches(msg, m.keys.Tab) {
			m.filtering = !m.filtering
			if m.filtering {
				m.searchInput.Placeholder = "Filter agents..."
				return m, m.searchInput.Focus()
			}
			m.searchInput.Placeholder = "Filter agents... (press tab to focus)"
			m.searchInput.Blur()
			return m, nil
		}

		if m.filtering {
			// --- Search Focus Mode ---
			// Only handle Esc to clear/blur, everything else goes to input
			if key.Matches(msg, m.keys.Close) {
				m.filtering = false
				m.searchInput.Placeholder = "Filter agents... (press tab to focus)"
				m.searchInput.Blur()
				return m, nil
			}

			// Pass all other keys to search input
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		// --- List Focus Mode ---
		// Handle navigation and actions
		switch {
		case key.Matches(msg, m.keys.Close):
			return m, nil // Handled by parent usually

		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case key.Matches(msg, m.keys.Down):
			// Need to check against filtered list length
			filtered := m.getFilteredAgents()
			if m.cursor < len(filtered)-1 {
				m.cursor++
			}
			return m, nil

		case key.Matches(msg, m.keys.Start):
			filtered := m.getFilteredAgents()
			if len(filtered) > 0 {
				agent := filtered[m.cursor]
				m.pending[agent.Name] = "Starting..."
				return m, m.startAgent(agent.Name)
			}

		case key.Matches(msg, m.keys.Stop):
			filtered := m.getFilteredAgents()
			if len(filtered) > 0 {
				agent := filtered[m.cursor]
				m.pending[agent.Name] = "Stopping..."
				return m, m.stopAgent(agent.Name)
			}

		case key.Matches(msg, m.keys.Restart):
			filtered := m.getFilteredAgents()
			if len(filtered) > 0 {
				agent := filtered[m.cursor]
				m.pending[agent.Name] = "Restarting..."
				return m, m.restartAgent(agent.Name)
			}

		case key.Matches(msg, m.keys.Enter):
			// Enter could select, or just do nothing/clear filter?
			return m, nil
		}

		return m, tea.Batch(cmds...)

	case spinner.TickMsg:
		var sCmd tea.Cmd
		m.spinner, sCmd = m.spinner.Update(msg)
		cmds = append(cmds, sCmd)

	case pollMsg:
		cmds = append(cmds, m.Refresh())
		cmds = append(cmds, m.pollUpdates())

	case agentsMsg:
		m.loading = false
		m.agents = msg.agents
		m.err = nil
		// Sort agents: running first, then by name
		sort.Slice(m.agents, func(i, j int) bool {
			ai := m.agents[i]
			aj := m.agents[j]
			if ai.Status == "running" && aj.Status != "running" {
				return true
			}
			if ai.Status != "running" && aj.Status == "running" {
				return false
			}
			return strings.ToLower(ai.Name) < strings.ToLower(aj.Name)
		})

		// Clear pending state if status matches expectation
		for name, action := range m.pending {
			for _, agent := range m.agents {
				if agent.Name == name {
					currentStatus := strings.ToLower(agent.Status)
					if action == "Starting..." && currentStatus == "running" {
						delete(m.pending, name)
					} else if action == "Stopping..." && (currentStatus == "stopped" || currentStatus == "crashed") {
						delete(m.pending, name)
					} else if action == "Restarting..." && currentStatus == "running" {
						delete(m.pending, name)
					}
				}
			}
		}

		// Re-validate cursor against new list (filtered)
		filtered := m.getFilteredAgents()
		if m.cursor >= len(filtered) && len(filtered) > 0 {
			m.cursor = len(filtered) - 1
		} else if len(filtered) == 0 {
			m.cursor = 0
		}

	case errMsg:
		m.loading = false
		m.err = msg.err

	case actionResultMsg:
		delete(m.pending, msg.agentName)
		if msg.err != nil {
			m.err = msg.err
		} else {
			// Trigger a refresh to get the new state
			cmds = append(cmds, m.Refresh())
		}

	default:
		// Handle other messages like cursor blink
		if m.filtering {
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) getFilteredAgents() []llm.AgentInfo {
	if m.searchInput.Value() == "" {
		return m.agents
	}
	filter := strings.ToLower(m.searchInput.Value())
	var filtered []llm.AgentInfo
	for _, a := range m.agents {
		if strings.Contains(strings.ToLower(a.Name), filter) ||
			strings.Contains(strings.ToLower(a.Description), filter) ||
			strings.Contains(strings.ToLower(a.Daemon), filter) {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

func (m *Model) startAgent(name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		args := fmt.Sprintf(`{"name": "%s"}`, name)
		res, _ := tooling.RunStartAgent(ctx, args)
		if strings.HasPrefix(res, "error:") {
			return actionResultMsg{agentName: name, err: fmt.Errorf("%s", res)}
		}
		llm.InvalidateAgentListCache()
		return actionResultMsg{agentName: name, err: nil}
	}
}

func (m *Model) stopAgent(name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		args := fmt.Sprintf(`{"name": "%s"}`, name)
		res, _ := tooling.RunStopAgent(ctx, args)
		if strings.HasPrefix(res, "error:") {
			return actionResultMsg{agentName: name, err: fmt.Errorf("%s", res)}
		}
		llm.InvalidateAgentListCache()
		return actionResultMsg{agentName: name, err: nil}
	}
}

func (m *Model) restartAgent(name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		args := fmt.Sprintf(`{"name": "%s"}`, name)
		res, _ := tooling.RunRestartAgent(ctx, args)
		if strings.HasPrefix(res, "error:") {
			return actionResultMsg{agentName: name, err: fmt.Errorf("%s", res)}
		}
		llm.InvalidateAgentListCache()
		return actionResultMsg{agentName: name, err: nil}
	}
}

func (m *Model) View() string {
	t := styles.CurrentTheme()
	s := t.S()
	leftOnly := lipgloss.Border{Left: "│"}

	// Calculate box width first
	targetW := 0
	if m.width > 0 {
		targetW = m.width / 2
		if targetW < 60 {
			targetW = 60
		}
		if targetW > 100 {
			targetW = 100
		}
		if targetW > m.width-6 {
			targetW = m.width - 6
		}
	}

	// Use filtered agents
	filteredAgents := m.getFilteredAgents()

	// Update search input width to match container
	// Box padding is 2 (left) + 2 (right) = 4
	// Border is 1 (left) + 1 (right) = 2
	// Total horizontal reduction = 6
	if targetW > 6 {
		m.searchInput.SetWidth(targetW - 6)
	}

	// Header is now the search bar
	header := m.searchInput.View()

	// Title
	title := s.Title.Render("Agent List")

	// Footer with help and optional error
	footer := s.Base.Render(m.help.View(m.keys))
	if m.err != nil && len(m.agents) > 0 {
		errorMsg := lipgloss.NewStyle().Foreground(t.Error).Render(fmt.Sprintf("Error: %v", m.err))
		footer = lipgloss.JoinVertical(lipgloss.Left, footer, "", errorMsg)
	}

	// Calculate content height for viewport
	// Total height - Title - Header - Footer - Padding(2) - Border(2)
	// We need to be careful with calculations.
	// Let's assume targetH is the total box height.
	// Viewport height = targetH - lipgloss.Height(title) - lipgloss.Height(header) - lipgloss.Height(footer) - 2 (padding) - 2 (border)

	targetH := 0
	if m.height > 0 {
		targetH = m.height / 2
		if targetH < 15 {
			targetH = 15
		}
		if targetH > 30 {
			targetH = 30
		}
		if targetH > m.height-6 {
			targetH = m.height - 6
		}
	}

	contentHeight := 0
	if targetH > 0 {
		contentHeight = targetH - lipgloss.Height(title) - lipgloss.Height(header) - lipgloss.Height(footer) - 4 // Approx padding/border
		if contentHeight < 5 {
			contentHeight = 5 // Minimum height
		}
	} else {
		contentHeight = 10 // Default fallback
	}

	if contentHeight%2 != 0 {
		contentHeight--
	}

	m.viewport.SetWidth(targetW - 4) // Adjust for padding/border
	m.viewport.SetHeight(contentHeight)

	// Render list content
	var listContent string
	if len(filteredAgents) == 0 {
		if m.loading {
			listContent = s.Base.PaddingLeft(1).Render("Loading agents...")
		} else if m.err != nil {
			listContent = s.Base.PaddingLeft(1).Foreground(t.Error).Render(fmt.Sprintf("Error loading agents: %v", m.err))
		} else {
			msg := "No agents found"
			if m.searchInput.Value() != "" {
				msg = "No agents match filter"
			}
			listContent = s.Base.PaddingLeft(1).Render(msg)
		}
	} else {
		items := make([]string, len(filteredAgents))

		// Calculate available width for items based on target box width
		// Box padding (1*2=2) + Border (1*2=2) + Item padding (1) + BorderLeft (1) = 6 (approx)
		// We'll use a safe margin from targetW
		itemWidth := targetW - 6
		if itemWidth < 1 {
			itemWidth = 1
		}

		for i, agent := range filteredAgents {
			// Always reserve space with a left border to avoid layout shift
			itemStyle := s.Base.
				PaddingLeft(1).
				BorderLeft(true).
				BorderStyle(leftOnly).
				Width(itemWidth) // Force full width

			// Only highlight if we are NOT filtering (list has focus) AND this is the selected item
			if !m.filtering && i == m.cursor {
				// Emphasize selected item
				itemStyle = itemStyle.BorderForeground(t.Primary)
			} else {
				itemStyle = itemStyle.BorderForeground(t.FgMuted)
			}

			// Status Icon & Color
			var statusIcon string
			var statusColor color.Color
			statusStyle := lipgloss.NewStyle()

			// Check pending state first
			if _, ok := m.pending[agent.Name]; ok {
				statusIcon = m.spinner.View()
				statusColor = t.Warning
				statusStyle = statusStyle.Foreground(t.Warning)
			} else {
				switch strings.ToLower(agent.Status) {
				case "running":
					statusIcon = "●"
					statusColor = t.Success
					statusStyle = statusStyle.Foreground(t.Success)
				case "stopped":
					statusIcon = "○"
					statusColor = t.FgMuted
					statusStyle = statusStyle.Foreground(t.FgMuted)
				case "crashed":
					statusIcon = "○"
					statusColor = t.Error
					statusStyle = statusStyle.Foreground(t.Error)
				default:
					statusIcon = "○"
					statusColor = t.FgSubtle
					statusStyle = statusStyle.Foreground(t.FgSubtle)
				}
			}

			// Line 1: Icon Status Name (Daemon)
			var line1 string
			if action, ok := m.pending[agent.Name]; ok {
				// Pending state - use the pulsing icon
				icon := lipgloss.NewStyle().Foreground(statusColor).Render(statusIcon)
				status := statusStyle.Render(action)
				name := lipgloss.NewStyle().Foreground(t.Secondary).Render(agent.Name)
				line1 = fmt.Sprintf("%s %s %s", icon, status, name)
			} else {
				// Normal state
				icon := lipgloss.NewStyle().Foreground(statusColor).Render(statusIcon)

				// Title Case Status
				statusTitle := strings.Title(strings.ToLower(agent.Status))
				status := statusStyle.Render(statusTitle)

				name := lipgloss.NewStyle().Foreground(t.Secondary).Render(agent.Name)
				line1 = fmt.Sprintf("%s %s %s", icon, status, name)

				// Append PID and Uptime for running agents
				if strings.ToLower(agent.Status) == "running" && agent.PID > 0 {
					// Split styling for PID/Uptime: Muted labels, Base values
					muted := lipgloss.NewStyle().Foreground(t.FgMuted)
					base := lipgloss.NewStyle().Foreground(t.FgBase)

					pidLabel := muted.Render("pid")
					pidVal := base.Render(fmt.Sprintf("%d", agent.PID))

					uptimePart := ""
					if agent.Uptime != "" {
						upLabel := muted.Render("up")
						upVal := base.Render(formatUptime(agent.Uptime))
						uptimePart = fmt.Sprintf(", %s %s", upLabel, upVal)
					}

					// (pid 123, up 1h)
					info := fmt.Sprintf("%s%s %s%s%s",
						muted.Render("("),
						pidLabel,
						pidVal,
						uptimePart,
						muted.Render(")"))

					line1 += " " + info
				}

				if agent.Daemon != "" && agent.Daemon != "local" {
					// Daemon text color - using Info (Blue) to make it distinct
					daemonText := fmt.Sprintf("@%s", agent.Daemon)
					line1 += " " + lipgloss.NewStyle().Foreground(t.Info).Render(daemonText)
				}
			}

			// Line 2: Description
			desc := agent.Description
			if desc == "" {
				desc = "-"
			}
			// Truncate description to one line based on width
			// Available width for desc is itemWidth - padding (2)
			descWidth := itemWidth - 4
			if descWidth > 0 && len(desc) > descWidth {
				desc = desc[:descWidth-3] + "..."
			}

			// Use a darker shade for description (FgMuted is usually darker/subtle)
			// Adding Faint() makes it even darker/more subtle
			line2 := lipgloss.NewStyle().Foreground(t.FgMuted).Faint(true).PaddingLeft(2).Render(desc)

			items[i] = itemStyle.Render(lipgloss.JoinVertical(lipgloss.Left, line1, line2))
		}
		listContent = lipgloss.JoinVertical(lipgloss.Left, items...)
	}

	m.viewport.SetContent(listContent)

	// Ensure cursor is visible in viewport
	// Calculate cursor Y position relative to list start (0-indexed)
	// Each item is 2 lines high
	itemHeight := 2
	cursorTop := m.cursor * itemHeight
	cursorBottom := cursorTop + itemHeight - 1

	// Scroll up if cursor top is above viewport
	if cursorTop < m.viewport.YOffset() {
		m.viewport.SetYOffset(cursorTop)
	}

	// Scroll down if cursor bottom is below viewport
	// viewport.Height() is the number of visible lines
	// We want cursorBottom to be visible, so it must be < YOffset + Height
	if cursorBottom >= m.viewport.YOffset()+m.viewport.Height() {
		// Set YOffset such that cursorBottom is the last visible line
		// YOffset = cursorBottom - Height + 1
		m.viewport.SetYOffset(cursorBottom - m.viewport.Height() + 1)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		header,
		"",
		m.viewport.View(),
		"",
		footer,
	)

	// Make the container medium-sized and constrain inside the window
	box := s.Base.Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocus).
		Padding(1, 2)

	if targetW > 0 {
		box = box.Width(targetW)
	}
	if targetH > 0 {
		box = box.Height(targetH)
	}

	return box.Render(content)
}

func formatUptime(raw string) string {
	// Try to parse as float (seconds)
	var seconds float64
	_, err := fmt.Sscanf(raw, "%f", &seconds)
	if err != nil {
		return raw // Return as is if not a number
	}

	d := time.Duration(seconds) * time.Second

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}
