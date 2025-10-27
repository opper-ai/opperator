package tui

import "github.com/charmbracelet/bubbles/v2/key"

type keyMap struct {
	Help          key.Binding
	Quit          key.Binding
	Newline       key.Binding
	FocusPrev     key.Binding
	FocusNext     key.Binding
	ClearFocus    key.Binding
	ToggleFocus   key.Binding
	Cancel        key.Binding
	Sessions      key.Binding
	SwitchAgent   key.Binding
	ToggleSidebar key.Binding
}

// dynamicKeyMap adapts the help bindings based on focus state.
type dynamicKeyMap struct {
	km            keyMap
	inputFocused  bool
	cancelVisible bool
}

func (d dynamicKeyMap) ShortHelp() []key.Binding {
	var keys []key.Binding
	if d.cancelVisible {
		keys = append(keys, d.km.Cancel)
	}
	keys = append(keys, d.km.Sessions, d.km.SwitchAgent)
	if d.inputFocused {
		keys = append(keys, d.km.Newline, d.km.ToggleFocus, d.km.Quit)
		return keys
	}
	keys = append(keys, d.km.FocusPrev, d.km.FocusNext, d.km.ToggleFocus, d.km.ClearFocus, d.km.Quit)
	return keys
}

func (d dynamicKeyMap) FullHelp() [][]key.Binding {
	if d.inputFocused {
		keys := []key.Binding{}
		if d.cancelVisible {
			keys = append(keys, d.km.Cancel)
		}
		keys = append(keys, d.km.Sessions, d.km.SwitchAgent, d.km.Newline, d.km.ToggleFocus, d.km.Quit)
		return [][]key.Binding{keys}
	}
	keys := []key.Binding{}
	if d.cancelVisible {
		keys = append(keys, d.km.Cancel)
	}
	keys = append(keys, d.km.Sessions, d.km.SwitchAgent, d.km.FocusPrev, d.km.FocusNext, d.km.ToggleFocus, d.km.ClearFocus, d.km.Quit)
	return [][]key.Binding{keys}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Sessions, k.SwitchAgent, k.Newline, k.FocusPrev, k.FocusNext, k.ToggleFocus, k.ClearFocus, k.Quit, k.Cancel}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Help, k.Sessions, k.SwitchAgent, k.Newline, k.FocusPrev, k.FocusNext, k.ToggleFocus, k.ClearFocus, k.Quit, k.Cancel}}
}

var defaultKeys = keyMap{
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Newline: key.NewBinding(
		key.WithKeys("ctrl+j"),
		key.WithHelp("ctrl+j", "new line"),
	),
	FocusPrev: key.NewBinding(
		key.WithKeys("k", "ctrl+up"),
		key.WithHelp("k", "focus prev msg"),
	),
	FocusNext: key.NewBinding(
		key.WithKeys("j", "ctrl+down"),
		key.WithHelp("j", "focus next msg"),
	),
	ClearFocus: key.NewBinding(
		key.WithKeys("c/y"),
		key.WithHelp("c/y", "copy content"),
	),
	ToggleFocus: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "toggle focus"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	Sessions: key.NewBinding(
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "sessions"),
	),
	SwitchAgent: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "next agent"),
	),
	ToggleSidebar: key.NewBinding(
		key.WithKeys("ctrl+b"),
		key.WithHelp("ctrl+b", "toggle sidebar"),
	),
}
