package sidebar

// NavigationState manages navigation and selection state for the sidebar
type NavigationState struct {
	selectedSection     Section
	selectedCustomIndex int // -1 if no custom section selected
}

// NewNavigationState creates a new navigation state
func NewNavigationState() *NavigationState {
	return &NavigationState{
		selectedSection:     SectionNone,
		selectedCustomIndex: -1,
	}
}

// GetSelectedSection returns the currently selected section
func (n *NavigationState) GetSelectedSection() Section {
	return n.selectedSection
}

// GetSelectedCustomIndex returns the currently selected custom section index
func (n *NavigationState) GetSelectedCustomIndex() int {
	return n.selectedCustomIndex
}

// SetSelectedSection sets the selected section and clears custom index
func (n *NavigationState) SetSelectedSection(section Section) {
	n.selectedSection = section
	n.selectedCustomIndex = -1
}

// SetSelectedCustomIndex sets the selected custom section index and clears built-in selection
func (n *NavigationState) SetSelectedCustomIndex(index int) {
	n.selectedCustomIndex = index
	n.selectedSection = SectionNone
}

// IsCustomSectionSelected returns true if a custom section is currently selected
func (n *NavigationState) IsCustomSectionSelected() bool {
	return n.selectedCustomIndex >= 0
}

// NavigationHelper provides navigation logic for the sidebar
type NavigationHelper struct {
	nav      *NavigationState
	agent    *AgentState
	builder  *BuilderState
	sections *SectionState
}

// NewNavigationHelper creates a new navigation helper
func NewNavigationHelper(nav *NavigationState, agent *AgentState, builder *BuilderState, sections *SectionState) *NavigationHelper {
	return &NavigationHelper{
		nav:      nav,
		agent:    agent,
		builder:  builder,
		sections: sections,
	}
}

// GetAvailableSections returns the list of available built-in sections based on current state
func (h *NavigationHelper) GetAvailableSections() []Section {
	sections := []Section{}

	if h.agent.Name != "" {
		sections = append(sections, SectionAgentInfo)

		if h.agent.Name == "Opperator" {
			sections = append(sections, SectionAgents)
		}

		if h.agent.Name == "Opperator" {
			// Opperator doesn't show additional sections
		} else if h.agent.Name == "Builder" {
			if h.builder.FocusedAgentName != "" {
				sections = append(sections, SectionTodos)
				sections = append(sections, SectionFocusedAgent)
				sections = append(sections, SectionTools)
			}
		} else {
			sections = append(sections, SectionTools)
		}

		sections = append(sections, SectionLogs)
	}

	return sections
}

// GetTotalSectionCount returns the total number of sections (built-in + custom)
func (h *NavigationHelper) GetTotalSectionCount() int {
	builtInCount := len(h.GetAvailableSections())
	return builtInCount + len(h.sections.CustomSections)
}

// FindSectionIndex finds the index of a section in the available sections list
func (h *NavigationHelper) FindSectionIndex(sections []Section, target Section) int {
	for i, section := range sections {
		if section == target {
			return i
		}
	}
	return -1
}

// ValidateAndFixSelection ensures the current selection is valid for available sections
func (h *NavigationHelper) ValidateAndFixSelection() {
	sections := h.GetAvailableSections()

	if len(sections) == 0 && len(h.sections.CustomSections) == 0 {
		h.nav.selectedSection = SectionNone
		h.nav.selectedCustomIndex = -1
		return
	}

	if h.nav.selectedCustomIndex >= 0 {
		if h.nav.selectedCustomIndex >= len(h.sections.CustomSections) {
			if len(sections) > 0 {
				h.nav.selectedSection = sections[0]
				h.nav.selectedCustomIndex = -1
			} else if len(h.sections.CustomSections) > 0 {
				h.nav.selectedCustomIndex = 0
				h.nav.selectedSection = SectionNone
			}
		}
		return
	}

	if h.nav.selectedSection != SectionNone {
		if h.FindSectionIndex(sections, h.nav.selectedSection) == -1 {
			if len(sections) > 0 {
				h.nav.selectedSection = sections[0]
			} else if len(h.sections.CustomSections) > 0 {
				h.nav.selectedCustomIndex = 0
				h.nav.selectedSection = SectionNone
			} else {
				h.nav.selectedSection = SectionNone
			}
		}
	} else {
		if len(sections) > 0 {
			h.nav.selectedSection = sections[0]
		} else if len(h.sections.CustomSections) > 0 {
			h.nav.selectedCustomIndex = 0
		}
	}
}

// FocusNext moves focus to the next section, returns true if focus changed
func (h *NavigationHelper) FocusNext() bool {
	sections := h.GetAvailableSections()
	totalCount := h.GetTotalSectionCount()

	if totalCount == 0 {
		return false
	}

	h.ValidateAndFixSelection()

	if h.nav.selectedCustomIndex >= 0 {
		if h.nav.selectedCustomIndex < len(h.sections.CustomSections)-1 {
			h.nav.selectedCustomIndex++
			return true
		}
		return false
	}

	currentIdx := h.FindSectionIndex(sections, h.nav.selectedSection)
	if currentIdx == -1 {
		if len(sections) > 0 {
			h.nav.selectedSection = sections[0]
		}
		return false
	}

	if currentIdx < len(sections)-1 {
		h.nav.selectedSection = sections[currentIdx+1]
		return true
	}

	if len(h.sections.CustomSections) > 0 {
		h.nav.selectedCustomIndex = 0
		h.nav.selectedSection = SectionNone
		return true
	}

	return false
}

// FocusPrev moves focus to the previous section, returns true if focus changed
func (h *NavigationHelper) FocusPrev() bool {
	sections := h.GetAvailableSections()
	totalCount := h.GetTotalSectionCount()

	if totalCount == 0 {
		return false
	}

	h.ValidateAndFixSelection()

	if h.nav.selectedCustomIndex >= 0 {
		if h.nav.selectedCustomIndex > 0 {
			h.nav.selectedCustomIndex--
			return true
		}

		if len(sections) > 0 {
			h.nav.selectedCustomIndex = -1
			h.nav.selectedSection = sections[len(sections)-1]
			return true
		}

		return false
	}

	currentIdx := h.FindSectionIndex(sections, h.nav.selectedSection)
	if currentIdx == -1 {
		if len(sections) > 0 {
			h.nav.selectedSection = sections[0]
		}
		return false
	}

	if currentIdx > 0 {
		h.nav.selectedSection = sections[currentIdx-1]
		return true
	}

	return false
}
