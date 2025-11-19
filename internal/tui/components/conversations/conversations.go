package conversations

import (
	"fmt"
	"strings"
	"tui/components/textarea"
	"tui/internal/conversation"
	"tui/internal/viewport"
	"tui/styles"

	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/spinner"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
)

type (
	SelectedMsg struct{ ID string }
	NewMsg      struct{}
	DeleteMsg   struct{ ID string }
	CloseMsg    struct{}
)

type Model struct {
	convs       []conversation.Conversation
	selected    int
	width       int
	height      int
	keyMap      KeyMap
	help        help.Model
	viewport    viewport.Model
	searchInput *textarea.Model
	filtering   bool
	spinner     spinner.Model
}

func New(convs []conversation.Conversation, currentID string) *Model {
	km := DefaultKeyMap()
	h := help.New()
	t := styles.CurrentTheme()
	h.Styles = t.S().Help

	ti := textarea.New()
	ti.Placeholder = "Filter conversations... (press tab to focus)"
	ti.Prompt = "┃ "
	ti.SetHeight(1)
	ti.ShowLineNumbers = false
	ti.CharLimit = 100

	// Match input.go styling
	st := t.S().TextArea
	st.Cursor.Blink = true
	st.Cursor.Shape = tea.CursorBar
	ti.SetStyles(st)

	s := spinner.New()
	s.Spinner = spinner.Spinner{
		Frames: []string{"●", "○"},
	}
	s.Style = lipgloss.NewStyle().Foreground(t.Warning)

	m := &Model{
		convs:       convs,
		width:       250,
		height:      60,
		keyMap:      km,
		help:        h,
		viewport:    viewport.New(),
		searchInput: ti,
		filtering:   false,
		spinner:     s,
	}
	for i, c := range convs {
		if c.ID == currentID {
			m.selected = i
			break
		}
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick)
}

func (m *Model) SetConversations(convs []conversation.Conversation) {
	m.convs = convs
	// Re-validate cursor
	filtered := m.getFilteredConversations()
	if m.selected >= len(filtered) && len(filtered) > 0 {
		m.selected = len(filtered) - 1
	} else if len(filtered) == 0 {
		m.selected = 0
	}
}

func (m *Model) IsFiltering() bool {
	return m.filtering
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.viewport.SetWidth(v.Width)
		m.viewport.SetHeight(v.Height)

	case tea.KeyMsg:
		// Tab always toggles focus
		if key.Matches(v, m.keyMap.Tab) {
			m.filtering = !m.filtering
			if m.filtering {
				m.searchInput.Placeholder = "Filter conversations..."
				return m, m.searchInput.Focus()
			}
			m.searchInput.Placeholder = "Filter conversations... (press tab to focus)"
			m.searchInput.Blur()
			return m, nil
		}

		if m.filtering {
			// --- Search Focus Mode ---
			if key.Matches(v, m.keyMap.Close) {
				m.filtering = false
				m.searchInput.Placeholder = "Filter conversations... (press tab to focus)"
				m.searchInput.Blur()
				return m, nil
			}

			// Pass keys to search input
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			cmds = append(cmds, cmd)

			// Reset selection when filtering changes
			filtered := m.getFilteredConversations()
			if m.selected >= len(filtered) {
				m.selected = 0
			}

			return m, tea.Batch(cmds...)
		}

		// --- List Focus Mode ---
		return m.handleKey(v)

	case tea.KeyPressMsg:
		return m.handleKey(v)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	default:
		// Handle cursor blink etc.
		if m.filtering {
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) handleKey(k fmt.Stringer) (tea.Model, tea.Cmd) {
	filtered := m.getFilteredConversations()

	switch {
	case key.Matches(k, m.keyMap.Previous):
		if m.selected > 0 {
			m.selected--
		}
	case key.Matches(k, m.keyMap.Next):
		if m.selected < len(filtered)-1 {
			m.selected++
		}
	case key.Matches(k, m.keyMap.Select):
		if len(filtered) > 0 {
			id := filtered[m.selected].ID
			return m, func() tea.Msg { return SelectedMsg{ID: id} }
		}
	case key.Matches(k, m.keyMap.New):
		return m, func() tea.Msg { return NewMsg{} }
	case key.Matches(k, m.keyMap.Delete):
		if len(filtered) > 0 {
			id := filtered[m.selected].ID
			return m, func() tea.Msg { return DeleteMsg{ID: id} }
		}
	case key.Matches(k, m.keyMap.Close):
		return m, func() tea.Msg { return CloseMsg{} }
	}
	return m, nil
}

func (m *Model) getFilteredConversations() []conversation.Conversation {
	if m.searchInput.Value() == "" {
		return m.convs
	}
	filter := strings.ToLower(m.searchInput.Value())
	var filtered []conversation.Conversation
	for _, c := range m.convs {
		if strings.Contains(strings.ToLower(c.Title), filter) ||
			strings.Contains(strings.ToLower(c.ActiveAgent), filter) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

func (m *Model) View() string {
	t := styles.CurrentTheme()
	s := t.S()
	leftOnly := lipgloss.Border{Left: "│"}

	// Calculate box width
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

	// Update search input width
	if targetW > 6 {
		m.searchInput.SetWidth(targetW - 6)
	}

	header := m.searchInput.View()
	title := s.Title.Render("Session History")

	// Footer
	footer := s.Base.Render(m.help.View(m.keyMap))

	// Calculate content height
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
		contentHeight = targetH - lipgloss.Height(title) - lipgloss.Height(header) - lipgloss.Height(footer) - 4
		if contentHeight < 5 {
			contentHeight = 5
		}
	} else {
		contentHeight = 10
	}

	m.viewport.SetWidth(targetW - 4)
	m.viewport.SetHeight(contentHeight)

	filtered := m.getFilteredConversations()
	var listContent string

	if len(filtered) == 0 {
		msg := "No conversations"
		if m.searchInput.Value() != "" {
			msg = "No conversations match filter"
		}
		listContent = s.Base.PaddingLeft(1).Render(msg)
	} else {
		items := make([]string, len(filtered))

		// Calculate available width for items - same as agent list
		// Box padding (1*2=2) + Border (1*2=2) + Item padding (1) + BorderLeft (1) = 6
		itemWidth := targetW - 6
		if itemWidth < 1 {
			itemWidth = 1
		}

		for i, c := range filtered {
			// Use the same approach as agent list
			itemStyle := s.Base.
				PaddingLeft(1).
				BorderLeft(true).
				BorderStyle(leftOnly).
				Width(itemWidth) // Force full width like agent list

			if !m.filtering && i == m.selected {
				itemStyle = itemStyle.BorderForeground(t.Primary)
			} else {
				itemStyle = itemStyle.BorderForeground(t.FgMuted)
			}

			// Build content: Title · Agent
			// Sanitize both title and agent
			title := strings.ReplaceAll(c.Title, "\n", " ")
			title = strings.TrimSpace(title)

			agent := strings.ReplaceAll(c.ActiveAgent, "\n", " ")
			agent = strings.TrimSpace(agent)

			// Render title in FgBase (neutral)
			titleRendered := lipgloss.NewStyle().Foreground(t.FgBase).Render(title)

			// Build the full content string
			var content string
			if agent != "" {
				// Render agent in Secondary (cyan)
				agentRendered := lipgloss.NewStyle().Foreground(t.Secondary).Render(agent)
				content = fmt.Sprintf("%s · %s", titleRendered, agentRendered)
			} else {
				content = titleRendered
			}

			items[i] = itemStyle.Render(content)
		}
		listContent = lipgloss.JoinVertical(lipgloss.Left, items...)
	}

	m.viewport.SetContent(listContent)

	// Scroll viewport to keep selected item visible
	itemHeight := 1 // Items are 1 line high here (unlike agent list which is 2)
	cursorTop := m.selected * itemHeight
	cursorBottom := cursorTop + itemHeight - 1

	if cursorTop < m.viewport.YOffset() {
		m.viewport.SetYOffset(cursorTop)
	}
	if cursorBottom >= m.viewport.YOffset()+m.viewport.Height() {
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
