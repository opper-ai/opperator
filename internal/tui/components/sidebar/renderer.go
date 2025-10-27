package sidebar

import (
	"tui/internal/protocol"
	"tui/styles"
)

// SectionRenderer is the interface for rendering individual sidebar sections
type SectionRenderer interface {
	// Render renders the section and adds it to the render state
	Render(state *SidebarRenderState, ctx *RenderContext)
}

// RenderContext contains all the data needed by section renderers
type RenderContext struct {
	// State components
	Agent    *AgentState
	Builder  *BuilderState
	Sections *SectionState

	// Dimensions
	Width        int
	SectionWidth int

	// UI state
	Theme           styles.Theme
	Focused         bool
	SelectedSection Section
	SelectedCustom  int // -1 if no custom section selected

	// Helper methods
	IsSelected func(section Section) bool
}

// NewRenderContext creates a new render context
func NewRenderContext(
	agent *AgentState,
	builder *BuilderState,
	sections *SectionState,
	width int,
	theme styles.Theme,
	focused bool,
	selectedSection Section,
	selectedCustom int,
) *RenderContext {
	sectionWidth := width - 4
	ctx := &RenderContext{
		Agent:           agent,
		Builder:         builder,
		Sections:        sections,
		Width:           width,
		SectionWidth:    sectionWidth,
		Theme:           theme,
		Focused:         focused,
		SelectedSection: selectedSection,
		SelectedCustom:  selectedCustom,
	}
	ctx.IsSelected = func(section Section) bool {
		return ctx.Focused && ctx.SelectedSection == section
	}
	return ctx
}

// CommandsForView splits commands into tools and slash commands
func CommandsForView(agent *AgentState, builder *BuilderState) (tools, slash []protocol.CommandDescriptor) {
	commands := agent.Commands
	if agent.Name == "Builder" && builder.FocusedAgentName != "" {
		commands = builder.FocusedAgentCommands
	}

	for _, cmd := range commands {
		isSlash := false
		for _, exposure := range cmd.ExposeAs {
			if exposure == protocol.CommandExposureSlashCommand {
				isSlash = true
				break
			}
		}
		if isSlash {
			slash = append(slash, cmd)
		} else {
			tools = append(tools, cmd)
		}
	}

	return
}
