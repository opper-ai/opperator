package sidebar

import (
	"github.com/charmbracelet/lipgloss/v2"
	"tui/styles"
)

// SidebarRenderState manages the rendering state for building the sidebar view
type SidebarRenderState struct {
	Theme            styles.Theme
	sections         []string
	lines            []string
	CumulativeHeight int
}

// NewSidebarRenderState creates a new render state with the given theme
func NewSidebarRenderState(theme styles.Theme) SidebarRenderState {
	return SidebarRenderState{
		Theme: theme,
	}
}

// AddSection adds a rendered section to the state
func (r *SidebarRenderState) AddSection(section string) {
	if section == "" {
		return
	}
	r.sections = append(r.sections, section)
	r.CumulativeHeight += lipgloss.Height(section)
}

// AddLine adds a single line to the state
func (r *SidebarRenderState) AddLine(line string) {
	r.lines = append(r.lines, line)
}

// Render renders all sections and lines into the final sidebar view
func (r *SidebarRenderState) Render(box lipgloss.Style) string {
	if len(r.sections) == 0 && len(r.lines) == 0 {
		return box.Render("")
	}

	if len(r.sections) > 0 {
		content := lipgloss.JoinVertical(lipgloss.Left, r.sections...)
		if len(r.lines) > 0 {
			content += "\n" + lipgloss.JoinVertical(lipgloss.Left, r.lines...)
		}
		return box.Render(content)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, r.lines...)
	return box.Render(content)
}
