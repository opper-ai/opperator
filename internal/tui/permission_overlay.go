package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"tui/permission"
	"tui/styles"
	tooltypes "tui/tools/types"
)

type permissionOverlay struct {
	req permission.PermissionRequest
	w   int

	focusIdx int
	options  []overlayOption
}

type overlayOption struct {
	label  string
	hint   string
	action permissionAction
}

var (
	allowBinding = key.NewBinding(
		key.WithKeys("y", "enter"),
		key.WithHelp("enter/y", "allow once"),
	)
	alwaysBinding = key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "always allow"),
	)
	denyBinding = key.NewBinding(
		key.WithKeys("n", "esc"),
		key.WithHelp("esc/n", "deny"),
	)
	moveBinding = key.NewBinding(
		key.WithKeys("up", "down", "j", "k", "ctrl+j", "ctrl+k"),
		key.WithHelp("↑/↓/j/k", "move"),
	)
)

type permissionHelpKeyMap struct{}

func (permissionHelpKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{allowBinding, alwaysBinding, denyBinding, moveBinding}
}

func (permissionHelpKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{allowBinding, alwaysBinding, denyBinding, moveBinding}}
}

type permissionAction int

const (
	actionOnce permissionAction = iota
	actionAlways
	actionDeny
)

func newPermissionOverlay(req permission.PermissionRequest) *permissionOverlay {
	return &permissionOverlay{
		req: req,
		options: []overlayOption{
			{label: "Yes", hint: "enter / y", action: actionOnce},
			{label: "Yes, and always allow", hint: "a", action: actionAlways},
			{label: "No, and tell Opperator what to do differently", hint: "n", action: actionDeny},
		},
	}
}

func (p *permissionOverlay) Request() permission.PermissionRequest { return p.req }

func (p *permissionOverlay) SetWidth(width int) { p.w = width }

func (p *permissionOverlay) FocusNext() {
	if len(p.options) == 0 {
		return
	}
	p.focusIdx = (p.focusIdx + 1) % len(p.options)
}

func (p *permissionOverlay) FocusPrev() {
	if len(p.options) == 0 {
		return
	}
	p.focusIdx = (p.focusIdx - 1 + len(p.options)) % len(p.options)
}

func (p *permissionOverlay) FocusedAction() permissionAction {
	if len(p.options) == 0 {
		return actionOnce
	}
	return p.options[p.focusIdx].action
}

func (p *permissionOverlay) Render() (string, int) {
	width := p.w / 2
	if width < 50 {
		width = 50
	}
	theme := styles.CurrentTheme()
	overlayBase := lipgloss.NewStyle().Background(theme.BgOverlay)
	textStyle := overlayBase.Copy().Foreground(theme.FgBase)
	mutedStyle := overlayBase.Copy().Foreground(theme.FgMuted)
	primaryStyle := overlayBase.Copy().Foreground(theme.Primary).Bold(true)

	tool := strings.ToUpper(strings.TrimSpace(p.req.ToolName))
	if tool == "" {
		tool = "TOOL"
	}
	action := strings.TrimSpace(p.req.Action)
	header := primaryStyle.Width(width).Render(tool)
	if action != "" {
		header = primaryStyle.Width(width - 1).Render(fmt.Sprintf("Permission needed"))
	}

	var infoLines []string
	if trimmed := strings.TrimSpace(p.req.Path); trimmed != "" {
		infoLines = append(infoLines, textStyle.Render(fmt.Sprintf("%s: %s", action, filepath.Clean(trimmed))))
	}
	if params := p.renderParams(textStyle, width); params != "" {
		infoLines = append(infoLines, params)
	}
	if reason := strings.TrimSpace(p.req.Reason); reason != "" {
		infoLines = append(infoLines, mutedStyle.Render(fmt.Sprintf("Reason: %s", reason)))
	}
	// 	infoLines = append(infoLines, mutedStyle.Render(fmt.Sprintf("  %s", desc)))
	// }
	if len(infoLines) > 0 {
		infoLines = append(infoLines, "")
	}

	prompt := textStyle.Bold(true).MarginBottom(1).Background(theme.BgOverlay).Render("Do you want to proceed?")
	menu := p.renderMenu(primaryStyle, textStyle, mutedStyle)

	body := lipgloss.JoinVertical(lipgloss.Left, append(infoLines, prompt, menu)...)

	box := lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Background(theme.BgOverlay).
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.BorderFocus).
		Foreground(theme.FgBase).
		MarginBottom(0).
		Padding(0, 1)

	view := lipgloss.JoinVertical(lipgloss.Left, header, body)
	rendered := box.Render(view)
	return rendered, lipgloss.Height(rendered)
}

func (p *permissionOverlay) renderParams(style lipgloss.Style, width int) string {
	params := p.req.Params
	if params == nil {
		return ""
	}
	data, err := json.MarshalIndent(params, "", "  ")
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for i := range lines {
		lines[i] = style.Render("" + lines[i])
	}
	return strings.Join(lines, "\n")
}

func (p *permissionOverlay) renderMenu(primary, text, muted lipgloss.Style) string {
	items := make([]string, len(p.options))
	for i, opt := range p.options {
		label := opt.label
		if i == p.focusIdx {
			prefix := primary.Render("❯")
			slash := text.Render(" ")
			labelStyled := primary.Render(label)
			hintStyled := ""
			items[i] = lipgloss.JoinHorizontal(lipgloss.Left, prefix, slash, labelStyled, hintStyled)
		} else {
			prefix := muted.Render(" ")
			slash := text.Render(" ")
			labelStyled := muted.Render(label)
			hintStyled := ""
			items[i] = lipgloss.JoinHorizontal(lipgloss.Left, prefix, slash, labelStyled, hintStyled)
		}
	}
	return strings.Join(items, "\n")
}

// ============================================================================
// Permission UI (consolidated from permission_ui.go)
// ============================================================================

type permissionCallbacks struct {
	grant         func(permission.PermissionRequest, bool)
	deny          func(permission.PermissionRequest)
	cancelSession func(string)
	focusInput    func() tea.Cmd
	blurInput     func() tea.Cmd
}

type permissionUI struct {
	overlay   *permissionOverlay
	width     int
	callbacks permissionCallbacks
}

func newPermissionUI(cb permissionCallbacks) *permissionUI {
	return &permissionUI{callbacks: cb}
}

func (p *permissionUI) active() bool { return p != nil && p.overlay != nil }

func (p *permissionUI) present(req permission.PermissionRequest) tea.Cmd {
	if p == nil {
		return nil
	}
	p.overlay = newPermissionOverlay(req)
	p.updateOverlayWidth()
	return p.blurInput()
}

func (p *permissionUI) clearIfMatches(toolCallID string) tea.Cmd {
	if p == nil || p.overlay == nil {
		return nil
	}
	if strings.TrimSpace(toolCallID) == "" {
		return nil
	}
	if p.overlay.Request().ToolCallID != toolCallID {
		return nil
	}
	p.overlay = nil
	return p.focusInput()
}

func (p *permissionUI) handleMsg(msg tea.Msg) (tea.Cmd, bool) {
	if p == nil || p.overlay == nil {
		return nil, false
	}
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		p.SetWidth(v.Width)
		return nil, true
	case tea.KeyMsg:
		return p.handleKey(strings.ToLower(v.String())), true
	case tea.KeyPressMsg:
		return p.handleKey(strings.ToLower(v.String())), true
	case tea.MouseMsg:
		return nil, true
	default:
		return nil, false
	}
}

func (p *permissionUI) handleKey(key string) tea.Cmd {
	if p == nil || p.overlay == nil {
		return nil
	}
	if p.handleNavigation(key) {
		return nil
	}
	switch key {
	case "y":
		p.overlay.focusIdx = 0
		return p.apply(actionOnce)
	case "a":
		if len(p.overlay.options) > 1 {
			p.overlay.focusIdx = 1
		}
		return p.apply(actionAlways)
	case "n", "esc":
		if len(p.overlay.options) > 0 {
			p.overlay.focusIdx = len(p.overlay.options) - 1
		}
		return p.apply(actionDeny)
	case "enter", " ":
		return p.apply(p.overlay.FocusedAction())
	default:
		return nil
	}
}

func (p *permissionUI) handleNavigation(key string) bool {
	if p == nil || p.overlay == nil {
		return false
	}
	switch key {
	case "down", "ctrl+n", "tab", "j", "ctrl+j":
		p.overlay.FocusNext()
		return true
	case "up", "ctrl+p", "shift+tab", "k", "ctrl+k":
		p.overlay.FocusPrev()
		return true
	default:
		return false
	}
}

func (p *permissionUI) apply(action permissionAction) tea.Cmd {
	if p == nil || p.overlay == nil {
		return nil
	}
	req := p.overlay.Request()
	switch action {
	case actionOnce:
		if p.callbacks.grant != nil {
			p.callbacks.grant(req, false)
		}
	case actionAlways:
		if p.callbacks.grant != nil {
			p.callbacks.grant(req, true)
		}
	case actionDeny:
		if p.callbacks.deny != nil {
			p.callbacks.deny(req)
		}
		if p.callbacks.cancelSession != nil {
			p.callbacks.cancelSession(req.SessionID)
		}
	default:
		return nil
	}
	p.overlay = nil
	return p.focusInput()
}

func (p *permissionUI) focusInput() tea.Cmd {
	if p == nil {
		return nil
	}
	if p.callbacks.focusInput == nil {
		return nil
	}
	return p.callbacks.focusInput()
}

func (p *permissionUI) blurInput() tea.Cmd {
	if p == nil {
		return nil
	}
	if p.callbacks.blurInput == nil {
		return nil
	}
	return p.callbacks.blurInput()
}

func (p *permissionUI) SetWidth(width int) {
	if p == nil {
		return
	}
	if width <= 0 {
		p.width = width
		return
	}
	if p.width == width {
		return
	}
	p.width = width
	p.updateOverlayWidth()
}

func (p *permissionUI) updateOverlayWidth() {
	if p == nil || p.overlay == nil {
		return
	}
	p.overlay.SetWidth(p.width)
}

func (p *permissionUI) render(base string, anchorHeight, xOffset int) string {
	if p == nil || p.overlay == nil {
		return base
	}
	view, height := p.overlay.Render()
	if view == "" {
		return base
	}
	y := anchorHeight + 1 - height
	if y < 0 {
		y = 0
	}
	return overlayString(base, view, xOffset, y)
}

func (p *permissionUI) currentRequest() (permission.PermissionRequest, bool) {
	if p == nil || p.overlay == nil {
		return permission.PermissionRequest{}, false
	}
	return p.overlay.Request(), true
}

func (p *permissionUI) helpKeyMap(defaultMap dynamicKeyMap) any {
	if p == nil || p.overlay == nil {
		return defaultMap
	}
	return permissionHelpKeyMap{}
}

func (p *permissionUI) ensureCleared() tea.Cmd {
	if p == nil || p.overlay == nil {
		return nil
	}
	p.overlay = nil
	return p.focusInput()
}

func (p *permissionUI) renderWithBase(base string, messagesView string, xOffset int) string {
	if p == nil || p.overlay == nil {
		return base
	}
	anchorHeight := lipgloss.Height(messagesView)
	return p.render(base, anchorHeight, xOffset)
}

// ============================================================================
// Model Methods (moved from model.go)
// ============================================================================

// handlePermissionOverlayMsg handles permission overlay messages
func (m *Model) handlePermissionOverlayMsg(msg tea.Msg) (tea.Cmd, bool) {
	ui := m.permissionUI()
	if ui == nil || !ui.active() {
		return nil, false
	}
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.handleWindowSizeMsg(ws)
	}
	return ui.handleMsg(msg)
}

// presentPermissionRequest presents a permission request to the user
func (m *Model) presentPermissionRequest(req permission.PermissionRequest) tea.Cmd {
	if strings.TrimSpace(req.SessionID) != "" && req.SessionID != m.sessionID {
		return nil
	}
	if m.permissionUI() == nil {
		return nil
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil
	}
	if m.pendingRequests == nil {
		m.pendingRequests = make(map[string]permission.PermissionRequest)
	}
	m.pendingRequests[id] = req
	return tea.Tick(permissionDialogDelay, func(time.Time) tea.Msg {
		return permissionDialogReadyMsg{requestID: id}
	})
}

// showPendingPermissionRequest shows a pending permission request
func (m *Model) showPendingPermissionRequest(id string) tea.Cmd {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	req, ok := m.pendingRequests[id]
	if !ok {
		return nil
	}
	delete(m.pendingRequests, id)
	if strings.TrimSpace(req.SessionID) != "" && req.SessionID != m.sessionID {
		return nil
	}
	ui := m.permissionUI()
	if ui == nil {
		return nil
	}
	var cmds []tea.Cmd
	if cmd := ui.present(req); cmd != nil {
		cmds = append(cmds, cmd)
	}
	m.updatePermissionOverlaySize()
	return batchCmds(cmds)
}

// handlePermissionRequestEvent handles permission request events from pubsub
func (m *Model) handlePermissionRequestEvent(msg permissionRequestEventMsg) tea.Cmd {
	var cmds []tea.Cmd
	if cmd := m.ensurePermissionToolPlaceholder(msg.Event.Payload); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if msg.Event.Payload.ToolCallID != "" {
		m.messages.MarkToolPermissionRequested(msg.Event.Payload.ToolCallID)
		if reason := strings.TrimSpace(msg.Event.Payload.Reason); reason != "" {
			m.setPendingToolReason(msg.Event.Payload.SessionID, msg.Event.Payload.ToolCallID, reason)
			if msg.Event.Payload.SessionID == "" || msg.Event.Payload.SessionID == m.sessionID {
				m.messages.SetToolReason(msg.Event.Payload.ToolCallID, reason)
			}
		}
	}
	if cmd := m.presentPermissionRequest(msg.Event.Payload); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if next := m.waitPermissionRequestEvent(); next != nil {
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

// ensurePermissionToolPlaceholder ensures a tool placeholder exists for the permission request
func (m *Model) ensurePermissionToolPlaceholder(req permission.PermissionRequest) tea.Cmd {
	id := strings.TrimSpace(req.ToolCallID)
	if id == "" {
		return nil
	}

	sessionID := strings.TrimSpace(req.SessionID)
	reason := strings.TrimSpace(req.Reason)
	name := strings.TrimSpace(req.ToolName)
	var cmds []tea.Cmd

	var input string
	if req.Params != nil {
		if encoded, err := json.Marshal(req.Params); err == nil {
			input = string(encoded)
		}
	}

	if calls, _ := m.streamManager().PendingToolCalls(sessionID); calls != nil {
		if existing, ok := calls[id]; ok {
			updated := false
			if reason != "" && strings.TrimSpace(existing.Reason) == "" {
				existing.Reason = reason
				updated = true
			}
			if input != "" && strings.TrimSpace(existing.Input) == "" {
				existing.Input = input
				updated = true
			}
			if updated {
				calls[id] = existing
				if sessionID == "" || sessionID == m.sessionID {
					if cmd := m.messages.EnsureToolCall(existing); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			}
			return batchCmds(cmds)
		}
	}

	call := tooltypes.Call{ID: id, Name: name, Reason: reason, Input: input}
	m.streamManager().TrackToolCall(sessionID, call)

	if sessionID != "" && sessionID != m.sessionID {
		return batchCmds(cmds)
	}

	if cmd := m.messages.EnsureToolCall(call); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return batchCmds(cmds)
}

// handlePermissionNotificationEvent handles permission notification events from pubsub
func (m *Model) handlePermissionNotificationEvent(msg permissionNotificationEventMsg) tea.Cmd {
	notif := msg.Event.Payload
	var cmds []tea.Cmd
	if notif.ToolCallID != "" && len(m.pendingRequests) > 0 && (notif.Granted || notif.Denied) {
		toolID := strings.TrimSpace(notif.ToolCallID)
		for id, req := range m.pendingRequests {
			if strings.TrimSpace(req.ToolCallID) == toolID {
				delete(m.pendingRequests, id)
			}
		}
	}
	ui := m.permissionUI()
	if notif.ToolCallID != "" {
		switch {
		case notif.Granted:
			m.messages.MarkToolPermissionGranted(notif.ToolCallID)
			if ui != nil {
				if cmd := ui.clearIfMatches(notif.ToolCallID); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		case notif.Denied:
			m.messages.MarkToolPermissionDenied(notif.ToolCallID)
			m.streamManager().ClearToolCallByID(notif.ToolCallID)
			if ui != nil {
				if cmd := ui.clearIfMatches(notif.ToolCallID); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		default:
			m.messages.MarkToolPermissionRequested(notif.ToolCallID)
		}
	}
	if next := m.waitPermissionNotificationEvent(); next != nil {
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

// grantPermission grants a permission request
func (m *Model) grantPermission(req permission.PermissionRequest, persistent bool) {
	if m.permissions == nil {
		return
	}
	if persistent {
		m.permissions.GrantPersistent(req)
	} else {
		m.permissions.Grant(req)
	}
}
