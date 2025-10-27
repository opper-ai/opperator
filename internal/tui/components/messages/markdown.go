package messages

import (
	"strings"

	"github.com/charmbracelet/glamour/v2"
	"tui/styles"
)

// renderer fails.
func renderMarkdown(width int, content string) string {
	if width < 1 {
		width = 1
	}

	theme := styles.CurrentTheme()

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(theme.S().Markdown),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return strings.TrimSuffix(content, "\n")
	}

	rendered, err := renderer.Render(content)
	if err != nil {
		return strings.TrimSuffix(content, "\n")
	}

	return strings.TrimSuffix(rendered, "\n")
}
