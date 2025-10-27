package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"

	cmpconversations "tui/components/conversations"
)

type keyEventContext struct {
	msg  tea.Msg
	key  string
	busy bool
}

type keyHandler func(*Model, keyEventContext) (tea.Cmd, bool)

func defaultKeyHandlers() map[string]keyHandler {
	return map[string]keyHandler{
		"ctrl+c":    handleQuitKey,
		"shift+tab": handleCycleAgentKey,
		"?":         handleHelpToggleKey,
		"tab":       handleTabKey,
		"ctrl+up":   handleFocusPrevKey,
		"ctrl+down": handleFocusNextKey,
		"k":         handlePrevMessageKey,
		"j":         handleNextMessageKey,
		"up":        handleHistoryPrevKey,
		"down":      handleHistoryNextKey,
		"esc":       handleEscapeKey,
		"ctrl+j":    handleNewlineKey,
		"ctrl+s":    handleSessionsKey,
		"ctrl+b":    handleToggleSidebarKey,
		"enter":     handleEnterKey,
		" ":         handleSpaceKey,
	}
}

func handleQuitKey(_ *Model, _ keyEventContext) (tea.Cmd, bool) {
	return tea.Quit, true
}

func handleCycleAgentKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	return m.cycleActiveAgent(), true
}

func handleHelpToggleKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	if m.input.IsFocused() {
		return nil, false
	}
	m.status.ToggleFullHelp()
	m.refreshHelp()
	return nil, true
}

func handleTabKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	// Cycle: Input -> Messages -> Sidebar (if visible) -> Input

	if m.sidebar.HasFocus() {
		m.sidebar.Blur()
		cmd := m.input.Focus()
		m.refreshHelp()
		return cmd, true
	}

	if m.messages.HasFocus() {
		m.messages.ClearFocus()
		if m.sidebarVisible {
			m.sidebar.Focus()
			cmd := m.input.Blur()
			m.refreshHelp()
			return cmd, true
		}
		cmd := m.input.Focus()
		m.refreshHelp()
		return cmd, true
	}

	if m.messages.FocusLast() {
		cmd := m.input.Blur()
		m.refreshHelp()
		return cmd, true
	}

	if m.sidebarVisible {
		m.sidebar.Focus()
		cmd := m.input.Blur()
		m.refreshHelp()
		return cmd, true
	}

	// Fallback: focus input
	cmd := m.input.Focus()
	m.refreshHelp()
	return cmd, true
}

func handleFocusPrevKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	if m.messages.FocusPrev() {
		cmd := m.input.Blur()
		m.refreshHelp()
		return cmd, true
	}
	return nil, false
}

func handleFocusNextKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	if m.messages.FocusNext() {
		cmd := m.input.Blur()
		m.refreshHelp()
		return cmd, true
	}
	return nil, false
}

func handlePrevMessageKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	if m.sidebar.HasFocus() {
		m.sidebar.FocusPrev()
		return nil, true
	}

	if !m.input.IsFocused() && m.messages.FocusPrev() {
		m.refreshHelp()
	}
	return nil, false
}

func handleNextMessageKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	if m.sidebar.HasFocus() {
		m.sidebar.FocusNext()
		return nil, true
	}

	if !m.input.IsFocused() && m.messages.FocusNext() {
		m.refreshHelp()
	}
	return nil, false
}

func handleHistoryPrevKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	if m.sidebar.HasFocus() {
		m.sidebar.FocusPrev()
		return nil, true
	}

	if !m.input.IsFocused() {
		return nil, false
	}
	val := m.input.Value()
	history := m.sessionManager().InputHistory()
	idx := m.sessionManager().HistoryIndex()
	if val == "" || idx < len(history) {
		if idx > 0 {
			idx--
			m.sessionManager().SetHistoryIndex(idx)
			m.input.SetValue(history[idx])
		}
		return nil, true
	}
	return nil, false
}

func handleHistoryNextKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	if m.sidebar.HasFocus() {
		m.sidebar.FocusNext()
		return nil, true
	}

	if !m.input.IsFocused() {
		return nil, false
	}
	val := m.input.Value()
	history := m.sessionManager().InputHistory()
	idx := m.sessionManager().HistoryIndex()
	if val == "" || idx < len(history) {
		if idx < len(history)-1 {
			idx++
			m.sessionManager().SetHistoryIndex(idx)
			m.input.SetValue(history[idx])
		} else if idx == len(history)-1 {
			m.sessionManager().SetHistoryIndex(len(history))
			m.input.SetValue("")
		}
		return nil, true
	}
	return nil, false
}

func handleEscapeKey(m *Model, ctx keyEventContext) (tea.Cmd, bool) {
	if m.toolDetail != nil {
		return m.closeToolDetail(), true
	}
	if ctx.busy {
		return m.cancel(), true
	}

	if m.sidebar.HasFocus() {
		m.sidebar.Blur()
		m.refreshHelp()
		return m.input.Focus(), true
	}

	m.messages.ClearFocus()
	m.refreshHelp()
	return m.input.Focus(), true
}

func handleNewlineKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	if m.input.IsFocused() {
		m.input.InsertNewline()
	}
	return nil, false
}

func handleSessionsKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	if m.convStore == nil {
		return nil, false
	}
	convs, _ := m.convStore.List(context.Background())
	m.convModal = cmpconversations.New(convs, m.sessionID)
	initCmd := m.convModal.Init()
	var sizeCmd tea.Cmd
	if m.w > 0 && m.h > 0 {
		sizeCmd = m.updateConvModal(tea.WindowSizeMsg{Width: m.w, Height: m.h})
	}
	return tea.Batch(initCmd, sizeCmd), true
}

func handleEnterKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	if m.sidebar.HasFocus() {
		m.sidebar.ToggleSection()
		return nil, true
	}

	if m.toolDetail != nil {
		return nil, true
	}
	if !m.input.IsFocused() {
		if m.messages != nil {
			if entry, ok := m.messages.FocusedToolEntry(); ok {
				return m.openToolDetail(entry.Call, entry.Result), true
			}
		}
		return nil, true
	}
	val := strings.TrimSpace(m.input.Value())
	return m.submitInput(val), true
}

func handleSpaceKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	if m.sidebar.HasFocus() {
		m.sidebar.ToggleSection()
		return nil, true
	}
	return nil, false
}

func handleToggleSidebarKey(m *Model, _ keyEventContext) (tea.Cmd, bool) {
	m.sidebarVisible = !m.sidebarVisible

	if m.sidebarVisible {
		_ = m.refreshSidebar()
	}

	if m.prefsStore != nil {
		_ = m.prefsStore.SetBool(context.Background(), "sidebar.visible", m.sidebarVisible)
	}

	layoutSimple(m)

	// Stats are now updated via event-based updates (agentStateEventMsg)

	return nil, true
}

// Main key event dispatcher

func (m *Model) handleKeyEvent(msg tea.Msg) (tea.Cmd, bool) {
	keyStr, ok := keyString(msg)
	if !ok {
		return nil, false
	}
	if m.agentPicker != nil {
		if cmd, handled := m.handleAgentPickerKey(keyStr); handled {
			return cmd, true
		}
	}

	busy := m.isSessionBusy(m.sessionID)
	state := m.streamState(m.sessionID)

	if (keyStr == "enter" || keyStr == "tab") && m.input.CommandPickerIsOpen() {
		if cmd, picked := m.input.SelectCommand(); picked {
			if strings.EqualFold(strings.TrimSpace(cmd.Name), "/agent") {
				m.input.SetValue("/agent ")
				if agentCmd := m.refreshAgentPickerFromInput(); agentCmd != nil {
					return agentCmd, true
				}
				return nil, true
			}
			if strings.EqualFold(strings.TrimSpace(cmd.Name), "/focus") {
				m.input.SetValue("/focus ")
				if agentCmd := m.refreshAgentPickerFromInput(); agentCmd != nil {
					return agentCmd, true
				}
				return nil, true
			}
			if cmd.RequiresArgument {
				return nil, true
			}
			val := strings.TrimSpace(cmd.Name)
			return m.submitInput(val), true
		}
		return nil, true
	}

	if (keyStr == "ctrl+k" || keyStr == "ctrl+j" || keyStr == "up" || keyStr == "down") && m.input.CommandPickerIsOpen() {
		return m.input.Update(msg), true
	}

	if busy && state != nil && state.Canceling && keyStr != "esc" {
		state.Canceling = false
		m.refreshHelp()
	}

	if m.keyHandlers == nil {
		m.keyHandlers = defaultKeyHandlers()
	}
	if handler, ok := m.keyHandlers[keyStr]; ok {
		return handler(m, keyEventContext{msg: msg, key: keyStr, busy: busy})
	}

	return nil, false
}
