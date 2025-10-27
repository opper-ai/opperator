package sidebar

import "tui/internal/viewport"

// ViewportState manages the logs viewport for scrollable display
type ViewportState struct {
	Viewport viewport.Model
	Inited   bool
	AutoScroll bool // Whether to auto-scroll logs to bottom on new messages
	Y        int  // Y position of logs section in sidebar
	Height   int  // Height of logs section
}

// NewViewportState creates a new ViewportState
func NewViewportState() *ViewportState {
	return &ViewportState{
		AutoScroll: true, // Auto-scroll enabled by default
	}
}

// Init initializes the viewport if not already initialized
func (v *ViewportState) Init() {
	if v.Inited {
		return
	}
	v.Viewport = viewport.New()
	v.Viewport.MouseWheelEnabled = true
	v.Viewport.MouseWheelDelta = 1 // Scroll 1 line at a time
	v.Viewport.SoftWrap = true
	v.Inited = true
}

// IsAtBottom returns true if the viewport is scrolled to the bottom
func (v *ViewportState) IsAtBottom() bool {
	if !v.Inited {
		return true
	}
	return v.Viewport.AtBottom()
}

// SetSize updates the viewport size
func (v *ViewportState) SetSize(width, height int) {
	if v.Inited {
		v.Viewport.SetWidth(width)
		v.Viewport.SetHeight(height)
	}
}

// SetContent updates the viewport content
func (v *ViewportState) SetContent(content string) {
	if v.Inited {
		v.Viewport.SetContent(content)
	}
}

// GotoBottom scrolls to the bottom of the viewport
func (v *ViewportState) GotoBottom() {
	if v.Inited {
		v.Viewport.GotoBottom()
	}
}

// YOffset returns the current Y offset
func (v *ViewportState) YOffset() int {
	if !v.Inited {
		return 0
	}
	return v.Viewport.YOffset()
}

// View returns the rendered viewport view
func (v *ViewportState) View() string {
	if !v.Inited {
		return ""
	}
	return v.Viewport.View()
}
