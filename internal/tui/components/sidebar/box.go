package sidebar

import (
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"tui/styles"
)

// BoxWithLabel is a UI component that renders a box with a label embedded in the top border
type BoxWithLabel struct {
	BoxStyle   lipgloss.Style
	LabelStyle lipgloss.Style
}

// NewBoxWithLabel creates a new BoxWithLabel with theme-appropriate styling
func NewBoxWithLabel(t styles.Theme, selected bool) BoxWithLabel {
	borderColor := t.Border
	if selected {
		borderColor = lipgloss.Color("#8a6a60")
	}

	return BoxWithLabel{
		BoxStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(0, 1),
		LabelStyle: lipgloss.NewStyle().
			PaddingLeft(1).
			PaddingRight(1),
	}
}

// Render renders the box with a label and content
func (b BoxWithLabel) Render(label, content string, width int) string {
	var (
		border         lipgloss.Border = b.BoxStyle.GetBorderStyle()
		topBorderStyle lipgloss.Style  = lipgloss.NewStyle().Foreground(b.BoxStyle.GetBorderTopForeground())
		topLeft        string          = topBorderStyle.Render(border.TopLeft)
		topRight       string          = topBorderStyle.Render(border.TopRight)
		renderedLabel  string          = b.LabelStyle.Render(label)
	)

	boxStyle := b.BoxStyle.Copy().BorderTop(false)
	if width > 0 {
		boxStyle = boxStyle.Width(width)
	}
	bottom := boxStyle.Render(content)

	actualWidth := lipgloss.Width(bottom)
	cellsShort := max(0, actualWidth-lipgloss.Width(topLeft+topRight+renderedLabel))
	gap := strings.Repeat(border.Top, cellsShort)
	top := topLeft + renderedLabel + topBorderStyle.Render(gap) + topRight

	return top + "\n" + bottom
}

// RenderWithScrollbar renders the box with a scrollbar on the right border
func (b BoxWithLabel) RenderWithScrollbar(label, content string, width int, viewportHeight, totalLines, yOffset int) string {
	// First render normally
	normalRender := b.Render(label, content, width)

	// If no scrollbar needed (all content visible), return normal render without any indicator
	if totalLines <= viewportHeight || viewportHeight <= 0 {
		return normalRender
	}

	t := styles.CurrentTheme()

	// Calculate how much content is scrollable
	maxYOffset := totalLines - viewportHeight

	// Calculate scrollbar dimensions
	scrollbarSize := 1

	// Calculate scrollbar position
	// When yOffset = 0 (top), scrollbarPos should be 0
	// When yOffset = maxYOffset (bottom), scrollbarPos should be viewportHeight - 1
	var scrollbarPos int
	if maxYOffset <= 0 {
		// Not scrollable, place at top
		scrollbarPos = 0
	} else {
		// Map yOffset [0, maxYOffset] to scrollbarPos [0, viewportHeight-1]
		scrollbarPos = (yOffset * (viewportHeight - 1)) / maxYOffset
		if scrollbarPos < 0 {
			scrollbarPos = 0
		}
		if scrollbarPos >= viewportHeight {
			scrollbarPos = viewportHeight - 1
		}
	}

	// Choose scrollbar character based on amount of scrollable content
	// If there are only a few extra lines (< 5), use full block
	// Otherwise use smaller block
	var scrollbarChar string
	if maxYOffset < 5 {
		scrollbarChar = "█" // Full block for small amounts of scrolling
	} else {
		scrollbarChar = "▄" // Lower half block for larger amounts
	}

	scrollbarStyle := lipgloss.NewStyle().Foreground(t.FgSubtle)
	trackStyle := lipgloss.NewStyle().Foreground(b.BoxStyle.GetBorderRightForeground())

	// Split into lines and overlay scrollbar on right border
	lines := strings.Split(normalRender, "\n")
	result := make([]string, len(lines))

	for i, line := range lines {
		if i == 0 {
			// Top border line - skip
			result[i] = line
		} else if i-1 < viewportHeight {
			// Content lines (i-1 because first line is top border)
			contentLineIdx := i - 1
			hasThumb := contentLineIdx >= scrollbarPos && contentLineIdx < scrollbarPos+scrollbarSize

			// Use ansi.Truncate to properly handle ANSI codes
			// Get the visible width of the line
			lineWidth := ansi.StringWidth(line)

			if lineWidth > 0 {
				// Truncate to lineWidth-1 to remove last visible character (the border)
				truncated := ansi.Truncate(line, lineWidth-1, "")

				// Append scrollbar character
				if hasThumb {
					result[i] = truncated + scrollbarStyle.Render(scrollbarChar)
				} else {
					result[i] = truncated + trackStyle.Render("│")
				}
			} else {
				result[i] = line
			}
		} else {
			// Bottom border or extra lines
			result[i] = line
		}
	}

	return strings.Join(result, "\n")
}
