package sidebar

import "tui/internal/viewport"

// CustomViewportState manages a viewport for custom sections
type CustomViewportState struct {
	Viewport viewport.Model
	Inited   bool
	Y        int  // Y position of section in sidebar
	Height   int  // Height of section
}

// NewCustomViewportState creates a new CustomViewportState
func NewCustomViewportState() *CustomViewportState {
	return &CustomViewportState{}
}

// Init initializes the viewport if not already initialized
func (v *CustomViewportState) Init() {
	if v.Inited {
		return
	}
	v.Viewport = viewport.New()
	v.Viewport.MouseWheelEnabled = true
	v.Viewport.MouseWheelDelta = 1 // Scroll 1 line at a time
	v.Viewport.SoftWrap = true
	v.Inited = true
}

// SetSize updates the viewport size
func (v *CustomViewportState) SetSize(width, height int) {
	if v.Inited {
		v.Viewport.SetWidth(width)
		v.Viewport.SetHeight(height)
	}
}

// SetContent updates the viewport content
func (v *CustomViewportState) SetContent(content string) {
	if v.Inited {
		v.Viewport.SetContent(content)
	}
}

// YOffset returns the current Y offset
func (v *CustomViewportState) YOffset() int {
	if !v.Inited {
		return 0
	}
	return v.Viewport.YOffset()
}

// TotalContentHeight returns the total height of the content
func (v *CustomViewportState) TotalContentHeight() int {
	if !v.Inited {
		return 0
	}
	return v.Viewport.TotalLineCount()
}

// View returns the rendered viewport view
func (v *CustomViewportState) View() string {
	if !v.Inited {
		return ""
	}
	return v.Viewport.View()
}
