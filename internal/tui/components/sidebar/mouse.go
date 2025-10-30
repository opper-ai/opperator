package sidebar

import tea "github.com/charmbracelet/bubbletea/v2"

// MouseHandler manages mouse interaction logic for the sidebar
type MouseHandler struct {
	sidebarX int
	width    int
	logs     *ViewportState
	sections *SectionState
}

// NewMouseHandler creates a new mouse handler
func NewMouseHandler(sidebarX, width int, logs *ViewportState, sections *SectionState) *MouseHandler {
	return &MouseHandler{
		sidebarX: sidebarX,
		width:    width,
		logs:     logs,
		sections: sections,
	}
}

// UpdatePosition updates the sidebar position and width
func (m *MouseHandler) UpdatePosition(x, width int) {
	m.sidebarX = x
	m.width = width
}

// IsMouseInSidebar checks if the mouse is within the sidebar bounds
func (m *MouseHandler) IsMouseInSidebar(msg tea.Msg) bool {
	if mouseMsg, ok := msg.(tea.MouseMsg); ok {
		mouse := mouseMsg.Mouse()
		sidebarLeft := m.sidebarX
		sidebarRight := m.sidebarX + m.width + 4 // +4 for margin and padding
		return mouse.X >= sidebarLeft && mouse.X < sidebarRight
	}
	return false
}

// HandleLogsViewportMouse handles mouse events for the logs viewport
// Returns a command if the viewport was updated, nil otherwise
func (m *MouseHandler) HandleLogsViewportMouse(msg tea.Msg) tea.Cmd {
	// Only handle if logs section is expanded and viewport is initialized
	if !m.sections.LogsExpanded || !m.logs.Inited {
		return nil
	}

	// Extract mouse information from different message types
	var mouse tea.Mouse
	var isMouseEvent bool

	switch m := msg.(type) {
	case tea.MouseWheelMsg:
		mouse = m.Mouse()
		isMouseEvent = true
	case tea.MouseClickMsg:
		mouse = m.Mouse()
		isMouseEvent = true
	case tea.MouseReleaseMsg:
		mouse = m.Mouse()
		isMouseEvent = true
	case tea.MouseMotionMsg:
		mouse = m.Mouse()
		isMouseEvent = true
	}

	if !isMouseEvent {
		return nil
	}

	// Check if mouse is within the sidebar area
	mouseX := mouse.X
	mouseY := mouse.Y

	sidebarLeft := m.sidebarX
	sidebarRight := m.sidebarX + m.width + 4 // +4 for margin and padding

	// Check if mouse X is within sidebar bounds
	if mouseX < sidebarLeft || mouseX >= sidebarRight {
		return nil
	}

	// Check if mouse Y is within the logs section
	logsTop := m.logs.Y
	logsBottom := m.logs.Y + m.logs.Height

	if mouseY < logsTop || mouseY >= logsBottom {
		// Mouse is outside logs section, don't update viewport
		return nil
	}

	// Mouse is within logs section bounds, update viewport
	oldYOffset := m.logs.YOffset()

	var cmd tea.Cmd
	m.logs.Viewport, cmd = m.logs.Viewport.Update(msg)

	// Detect if user manually scrolled
	if m.logs.YOffset() != oldYOffset {
		// User scrolled - check if they're at the bottom
		m.logs.AutoScroll = m.logs.IsAtBottom()
	}

	// Note: Don't mark sidebar dirty - viewport handles its own rendering
	// Marking dirty here causes full sidebar re-render on every scroll (expensive!)
	return cmd
}

// HandleCustomSectionViewportsMouse handles mouse events for custom section viewports
// Returns a command if any viewport was updated, nil otherwise
func (m *MouseHandler) HandleCustomSectionViewportsMouse(msg tea.Msg, customViewports map[string]*CustomViewportState) tea.Cmd {
	// Extract mouse information from different message types
	var mouse tea.Mouse
	var isMouseEvent bool

	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		mouse = msg.Mouse()
		isMouseEvent = true
	case tea.MouseClickMsg:
		mouse = msg.Mouse()
		isMouseEvent = true
	case tea.MouseReleaseMsg:
		mouse = msg.Mouse()
		isMouseEvent = true
	case tea.MouseMotionMsg:
		mouse = msg.Mouse()
		isMouseEvent = true
	}

	if !isMouseEvent {
		return nil
	}

	// Check if mouse is within the sidebar area
	mouseX := mouse.X
	mouseY := mouse.Y

	sidebarLeft := m.sidebarX
	sidebarRight := m.sidebarX + m.width + 4 // +4 for margin and padding

	// Check if mouse X is within sidebar bounds
	if mouseX < sidebarLeft || mouseX >= sidebarRight {
		return nil
	}

	// Check each custom section viewport
	for _, section := range m.sections.CustomSections {
		// Only check if section is expanded
		if !m.sections.CustomSectionsExpanded[section.ID] {
			continue
		}

		vp, exists := customViewports[section.ID]
		if !exists || !vp.Inited {
			continue
		}

		// Check if mouse Y is within this custom section
		sectionTop := vp.Y
		sectionBottom := vp.Y + vp.Height

		if mouseY >= sectionTop && mouseY < sectionBottom {
			// Mouse is within this section bounds, update viewport
			var cmd tea.Cmd
			vp.Viewport, cmd = vp.Viewport.Update(msg)
			return cmd
		}
	}

	return nil
}
