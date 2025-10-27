package tui

// layoutSimple allocates space and calls SetSize on components.
// Separation keeps visual math away from Update logic.
func layoutSimple(m *Model) {
	const (
		statsW      = 24
		sidebarW    = 60
		minBody     = 5
		inputH      = 3
		inputPad    = 3
		headerH     = 1
		headerPad   = 1
		minStatusH  = 3 // Reserve minimum height for status to prevent layout shifts
	)

	w, h := m.w, m.h

	// Sidebar takes space from everything when visible
	mainW := w
	if m.sidebarVisible {
		mainW = w - sidebarW
	}

	if m.header != nil {
		if m.sidebarVisible {
			m.header.SetWidth(mainW)
		} else {
			m.header.SetWidth(mainW - statsW)
		}
	}

	actualHeaderH := 1 // Header row height
	bodyH := h - actualHeaderH - m.helpH - inputH - inputPad
	if bodyH < minBody {
		bodyH = minBody
	}

	// Messages take the full main width (no right panel in body)
	m.messages.SetSize(mainW, bodyH)

	// Stats are always sized for header display (right side of header)
	m.stats.SetSize(statsW, 1)

	// Sidebar takes full height when visible
	if m.sidebarVisible {
		m.sidebar.SetSize(sidebarW, h)
		m.sidebar.SetPosition(mainW) // Set X position for mouse bounds checking
	} else {
		m.sidebar.SetSize(0, 0)
	}

	// Input takes the main area width
	m.input.SetSize(mainW, inputH)

	// Status takes the main area width
	m.status.SetWidth(mainW)
}
