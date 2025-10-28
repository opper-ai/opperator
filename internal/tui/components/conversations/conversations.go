package conversations

import (
	"fmt"
	"strings"
	"tui/internal/conversation"
	"tui/styles"

	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/key"
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
	convs    []conversation.Conversation
	selected int
	width    int
	height   int
	keyMap   KeyMap
	help     help.Model
}

func New(convs []conversation.Conversation, currentID string) *Model {
	km := DefaultKeyMap()
	h := help.New()
	t := styles.CurrentTheme()
	h.Styles = t.S().Help
	m := &Model{convs: convs, width: 250, height: 60, keyMap: km, help: h}
	for i, c := range convs {
		if c.ID == currentID {
			m.selected = i
			break
		}
	}
	return m
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) SetConversations(convs []conversation.Conversation) {
	m.convs = convs
	if m.selected >= len(convs) {
		m.selected = 0
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
	case tea.KeyMsg:
		return m.handleKey(v)
	case tea.KeyPressMsg:
		return m.handleKey(v)
	}
	return m, nil
}

func (m *Model) handleKey(k fmt.Stringer) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(k, m.keyMap.Previous):
		if m.selected > 0 {
			m.selected--
		}
	case key.Matches(k, m.keyMap.Next):
		if m.selected < len(m.convs)-1 {
			m.selected++
		}
	case key.Matches(k, m.keyMap.Select):
		if len(m.convs) > 0 {
			id := m.convs[m.selected].ID
			return m, func() tea.Msg { return SelectedMsg{ID: id} }
		}
	case key.Matches(k, m.keyMap.New):
		return m, func() tea.Msg { return NewMsg{} }
	case key.Matches(k, m.keyMap.Delete):
		if len(m.convs) > 0 {
			id := m.convs[m.selected].ID
			return m, func() tea.Msg { return DeleteMsg{ID: id} }
		}
	case key.Matches(k, m.keyMap.Close):
		return m, func() tea.Msg { return CloseMsg{} }
	}
	return m, nil
}

func (m *Model) View() string {
	t := styles.CurrentTheme()
	s := t.S()
	leftOnly := lipgloss.Border{Left: "▌"}

	var list string
	if len(m.convs) == 0 {
		list = s.Base.PaddingLeft(1).Render("No conversations")
	} else {
		items := make([]string, len(m.convs))
		for i, c := range m.convs {
			// Always reserve space with a left border to avoid layout shift
			itemStyle := s.Base.
				PaddingLeft(1).
				BorderLeft(true).
				BorderStyle(leftOnly)
			if i == m.selected {
				// Emphasize selected item
				itemStyle = itemStyle.BorderForeground(t.Primary)
			} else {
				itemStyle = itemStyle.BorderForeground(t.FgMuted)
			}
			label := c.Title
			if agent := strings.TrimSpace(c.ActiveAgent); agent != "" {
				label = fmt.Sprintf("%s · %s", label, agent)
			}
			items[i] = itemStyle.Render(label)
		}
		list = lipgloss.JoinVertical(lipgloss.Left, items...)
	}

	title := s.Title.PaddingLeft(1).Render("Session history")
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		list,
		"",
		s.Base.PaddingLeft(1).Render(m.help.View(m.keyMap)),
	)

	// Make the container medium-sized and constrain inside the window
	box := s.Base.Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocus).
		Padding(1, 2)
	if m.width > 0 {
		targetW := m.width / 2
		if targetW < 40 {
			targetW = 40
		}
		if targetW > 72 {
			targetW = 72
		}
		if targetW > m.width-6 {
			targetW = m.width - 6
		}
		if targetW > 0 {
			box = box.Width(targetW)
		}
	}
	if m.height > 0 {
		targetH := m.height / 2
		if targetH < 12 {
			targetH = 12
		}
		if targetH > 24 {
			targetH = 24
		}
		if targetH > m.height-6 {
			targetH = m.height - 6
		}
		if targetH > 0 {
			box = box.Height(targetH)
		}
	}

	return box.Render(content)
}
