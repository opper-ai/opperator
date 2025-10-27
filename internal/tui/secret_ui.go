package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/textinput"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"tui/internal/pubsub"
	"tui/secretprompt"
	"tui/styles"
)

type secretPromptCallbacks struct {
	submit     func(secretprompt.PromptRequest, string)
	cancel     func(secretprompt.PromptRequest)
	focusInput func() tea.Cmd
	blurInput  func() tea.Cmd
}

var (
	secretSubmitBinding = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "store secret"),
	)
	secretCancelBinding = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	)
)

type secretPromptHelpKeyMap struct{}

func (secretPromptHelpKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{secretSubmitBinding, secretCancelBinding}
}

func (secretPromptHelpKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{secretSubmitBinding, secretCancelBinding}}
}

type secretPromptUI struct {
	req       secretprompt.PromptRequest
	input     textinput.Model
	error     string
	visible   bool
	width     int
	callbacks secretPromptCallbacks
}

func newSecretPromptUI(cb secretPromptCallbacks) *secretPromptUI {
	ti := textinput.New()
	ti.Prompt = ""
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 0

	return &secretPromptUI{input: ti, callbacks: cb}
}

func (s *secretPromptUI) active() bool { return s != nil && s.visible }

func (s *secretPromptUI) Request() secretprompt.PromptRequest { return s.req }

func (s *secretPromptUI) present(req secretprompt.PromptRequest) tea.Cmd {
	if s == nil {
		return nil
	}
	s.req = req
	s.error = strings.TrimSpace(req.Error)
	s.visible = true
	placeholder := strings.TrimSpace(req.ValueLabel)
	if placeholder == "" {
		placeholder = "Secret value"
	}
	s.input.Placeholder = placeholder
	s.input.SetWidth(max(1, s.width-6))
	s.input.SetValue(req.DefaultValue)
	s.input.CursorEnd()

	focusCmd := s.input.Focus()
	cmds := []tea.Cmd{focusCmd}
	if s.callbacks.blurInput != nil {
		cmds = append(cmds, s.callbacks.blurInput())
	}
	return tea.Batch(cmds...)
}

func (s *secretPromptUI) dismiss() tea.Cmd {
	if s == nil || !s.visible {
		return nil
	}
	s.visible = false
	s.input.Reset()
	s.input.Blur()
	if s.callbacks.focusInput != nil {
		return s.callbacks.focusInput()
	}
	return nil
}

func (s *secretPromptUI) handleMsg(msg tea.Msg) (tea.Cmd, bool) {
	if s == nil || !s.visible {
		return nil, false
	}
	switch v := msg.(type) {
	case tea.KeyMsg:
		switch v.String() {
		case "enter":
			value := s.input.Value()
			if s.callbacks.submit != nil {
				s.callbacks.submit(s.req, value)
			}
			cmd := s.dismiss()
			return cmd, true
		case "esc", "ctrl+c":
			if s.callbacks.cancel != nil {
				s.callbacks.cancel(s.req)
			}
			cmd := s.dismiss()
			return cmd, true
		}
	case tea.KeyPressMsg:
		return nil, true
	}

	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return cmd, true
}

func (s *secretPromptUI) SetWidth(width int) {
	if s == nil {
		return
	}
	s.width = width
	if width > 0 {
		s.input.SetWidth(max(1, width-6))
	}
}

func (s *secretPromptUI) render(base string, anchorHeight, xOffset int) string {
	if s == nil || !s.visible {
		return base
	}
	t := styles.CurrentTheme()
	boxWidth := 60
	if boxWidth <= 0 {
		boxWidth = 60
	}
	if boxWidth < 40 {
		boxWidth = 30
	}

	title := strings.TrimSpace(s.req.Title)
	if title == "" {
		title = "Store secret"
	}
	header := lipgloss.NewStyle().Foreground(t.Primary).Bold(true).Render(title)
	desc := strings.TrimSpace(s.req.Description)
	var descView string
	if desc != "" {
		descView = lipgloss.NewStyle().Foreground(t.FgMuted).Render(desc)
	}

	var errorView string
	if s.error != "" {
		errorView = lipgloss.NewStyle().Foreground(t.Error).Render(s.error)
	}

	var docView string
	if trimmed := strings.TrimSpace(s.req.DocumentationURL); trimmed != "" {
		docView = lipgloss.NewStyle().Foreground(t.Secondary).Render("Need help? " + trimmed)
	}

	inputView := lipgloss.NewStyle().Width(boxWidth - 4).Render(s.input.View())
	help := lipgloss.NewStyle().Foreground(t.FgMutedMore).Render("enter to save • esc to cancel")

	segments := []string{header}
	if descView != "" {
		segments = append(segments, "", descView)
	}
	if errorView != "" {
		segments = append(segments, "", errorView)
	}
	segments = append(segments, "", inputView)
	if docView != "" {
		segments = append(segments, "", docView)
	}
	segments = append(segments, "", help)

	body := lipgloss.JoinVertical(lipgloss.Left, segments...)
	panel := lipgloss.NewStyle().
		Width(boxWidth).
		MaxWidth(boxWidth).
		Padding(1, 2).
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.BorderFocus).
		Background(t.BgOverlay).
		Foreground(t.FgBase).
		Render(body)

	y := anchorHeight + 1 - lipgloss.Height(panel)
	if y < 0 {
		y = 0
	}
	return overlayString(base, panel, xOffset, y)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ============================================================================
// Model Methods (moved from model.go)
// ============================================================================

// handleSecretPromptMsg handles secret prompt messages
func (m *Model) handleSecretPromptMsg(msg tea.Msg) (tea.Cmd, bool) {
	ui := m.secretPromptUI()
	if ui == nil || !ui.active() {
		return nil, false
	}
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.handleWindowSizeMsg(ws)
	}
	return ui.handleMsg(msg)
}

// presentSecretPrompt presents a secret prompt to the user
func (m *Model) presentSecretPrompt(req secretprompt.PromptRequest) tea.Cmd {
	if strings.TrimSpace(req.SessionID) != "" && req.SessionID != m.sessionID {
		return nil
	}
	ui := m.secretPromptUI()
	if ui == nil {
		return nil
	}
	m.updateSecretPromptOverlaySize()
	return ui.present(req)
}

// handleSecretPromptEvent handles secret prompt events from pubsub
func (m *Model) handleSecretPromptEvent(msg secretPromptEventMsg) tea.Cmd {
	var cmds []tea.Cmd
	switch msg.Event.Type {
	case pubsub.CreatedEvent:
		if cmd := m.presentSecretPrompt(msg.Event.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.DeletedEvent:
		if ui := m.secretPromptUI(); ui != nil && ui.active() {
			if strings.TrimSpace(ui.Request().ID) == strings.TrimSpace(msg.Event.Payload.ID) {
				if cmd := ui.dismiss(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	}
	if next := m.waitSecretPromptEvent(); next != nil {
		cmds = append(cmds, next)
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
