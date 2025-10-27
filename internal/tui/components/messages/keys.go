package messages

import "github.com/charmbracelet/bubbles/v2/key"

// CopyKey is the key binding for copying message/tool content to clipboard.
var CopyKey = key.NewBinding(
	key.WithKeys("c", "y", "C", "Y"),
	key.WithHelp("c/y", "copy"),
)

// ClearSelectionKey is the key binding for clearing text selection.
var ClearSelectionKey = key.NewBinding(
	key.WithKeys("esc"),
	key.WithHelp("esc", "clear selection"),
)
