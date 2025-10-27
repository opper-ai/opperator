package sidebar

import (
	"context"
	"fmt"
)

// SectionState manages the expand/collapse state of all sidebar sections
type SectionState struct {
	AgentInfoExpanded        bool
	AgentsExpanded           bool
	FocusedAgentInfoExpanded bool
	TodosExpanded            bool
	ToolsExpanded            bool
	SlashCommandsExpanded    bool
	LogsExpanded             bool
	CustomSectionsExpanded   map[string]bool
	CustomSections           []CustomSection
	CustomSectionsOrder      map[string]int // Tracks registration order by ID
}

// NewSectionState creates a new SectionState
func NewSectionState() *SectionState {
	return &SectionState{
		CustomSectionsExpanded: make(map[string]bool),
		CustomSectionsOrder:    make(map[string]int),
		CustomSections:         make([]CustomSection, 0),
	}
}

// LoadPreferences loads expand/collapse state from the preferences store
func (s *SectionState) LoadPreferences(prefsStore PreferencesStore) {
	if prefsStore == nil {
		return
	}

	ctx := context.Background()
	if expanded, err := prefsStore.GetBool(ctx, prefKeyAgentInfo); err == nil {
		s.AgentInfoExpanded = expanded
	}
	if expanded, err := prefsStore.GetBool(ctx, prefKeyAgents); err == nil {
		s.AgentsExpanded = expanded
	}
	if expanded, err := prefsStore.GetBool(ctx, prefKeyFocusedAgentInfo); err == nil {
		s.FocusedAgentInfoExpanded = expanded
	}
	if expanded, err := prefsStore.GetBool(ctx, prefKeyTodos); err == nil {
		s.TodosExpanded = expanded
	}
	if expanded, err := prefsStore.GetBool(ctx, prefKeyTools); err == nil {
		s.ToolsExpanded = expanded
	}
	if expanded, err := prefsStore.GetBool(ctx, prefKeySlashCommands); err == nil {
		s.SlashCommandsExpanded = expanded
	}
	if expanded, err := prefsStore.GetBool(ctx, prefKeyLogs); err == nil {
		s.LogsExpanded = expanded
	}
}

// ToggleAgentInfo toggles the agent info section
func (s *SectionState) ToggleAgentInfo(prefsStore PreferencesStore) {
	s.AgentInfoExpanded = !s.AgentInfoExpanded
	if prefsStore != nil {
		_ = prefsStore.SetBool(context.Background(), prefKeyAgentInfo, s.AgentInfoExpanded)
	}
}

// ToggleAgents toggles the agents list section
func (s *SectionState) ToggleAgents(prefsStore PreferencesStore) {
	s.AgentsExpanded = !s.AgentsExpanded
	if prefsStore != nil {
		_ = prefsStore.SetBool(context.Background(), prefKeyAgents, s.AgentsExpanded)
	}
}

// ToggleFocusedAgentInfo toggles the focused agent info section
func (s *SectionState) ToggleFocusedAgentInfo(prefsStore PreferencesStore) {
	s.FocusedAgentInfoExpanded = !s.FocusedAgentInfoExpanded
	if prefsStore != nil {
		_ = prefsStore.SetBool(context.Background(), prefKeyFocusedAgentInfo, s.FocusedAgentInfoExpanded)
	}
}

// ToggleTodos toggles the todos section
func (s *SectionState) ToggleTodos(prefsStore PreferencesStore) {
	s.TodosExpanded = !s.TodosExpanded
	if prefsStore != nil {
		_ = prefsStore.SetBool(context.Background(), prefKeyTodos, s.TodosExpanded)
	}
}

// ToggleTools toggles the tools section
func (s *SectionState) ToggleTools(prefsStore PreferencesStore) {
	s.ToolsExpanded = !s.ToolsExpanded
	if prefsStore != nil {
		_ = prefsStore.SetBool(context.Background(), prefKeyTools, s.ToolsExpanded)
	}
}

// ToggleLogs toggles the logs section
func (s *SectionState) ToggleLogs(prefsStore PreferencesStore) {
	s.LogsExpanded = !s.LogsExpanded
	if prefsStore != nil {
		_ = prefsStore.SetBool(context.Background(), prefKeyLogs, s.LogsExpanded)
	}
}

// ToggleCustomSection toggles a custom section by ID
func (s *SectionState) ToggleCustomSection(sectionID string, prefsStore PreferencesStore) {
	currentState := s.CustomSectionsExpanded[sectionID]
	newState := !currentState
	s.CustomSectionsExpanded[sectionID] = newState

	if prefsStore != nil {
		prefKey := fmt.Sprintf("sidebar.custom.%s.expanded", sectionID)
		_ = prefsStore.SetBool(context.Background(), prefKey, newState)
	}
}

// SetCustomSections updates the custom sections list
func (s *SectionState) SetCustomSections(sections []CustomSection, prefsStore PreferencesStore) (changed bool) {
	if customSectionsEqual(s.CustomSections, sections) {
		return false
	}

	// Track registration order for new sections
	nextOrder := len(s.CustomSectionsOrder)
	for i := range sections {
		section := &sections[i]
		if _, exists := s.CustomSectionsOrder[section.ID]; !exists {
			s.CustomSectionsOrder[section.ID] = nextOrder
			nextOrder++
		}
	}

	// Sort sections by their registration order
	sortedSections := make([]CustomSection, len(sections))
	copy(sortedSections, sections)

	for i := 0; i < len(sortedSections); i++ {
		for j := i + 1; j < len(sortedSections); j++ {
			orderI := s.CustomSectionsOrder[sortedSections[i].ID]
			orderJ := s.CustomSectionsOrder[sortedSections[j].ID]
			if orderI > orderJ {
				sortedSections[i], sortedSections[j] = sortedSections[j], sortedSections[i]
			}
		}
	}

	s.CustomSections = sortedSections

	// Load expanded state from preferences
	if prefsStore != nil {
		ctx := context.Background()
		for i := range s.CustomSections {
			section := &s.CustomSections[i]
			if _, exists := s.CustomSectionsExpanded[section.ID]; !exists {
				prefKey := fmt.Sprintf("sidebar.custom.%s.expanded", section.ID)
				if expanded, err := prefsStore.GetBool(ctx, prefKey); err == nil {
					s.CustomSectionsExpanded[section.ID] = expanded
				} else {
					s.CustomSectionsExpanded[section.ID] = !section.Collapsed
				}
			}
		}
	} else {
		for i := range s.CustomSections {
			section := &s.CustomSections[i]
			if _, exists := s.CustomSectionsExpanded[section.ID]; !exists {
				s.CustomSectionsExpanded[section.ID] = !section.Collapsed
			}
		}
	}

	return true
}
