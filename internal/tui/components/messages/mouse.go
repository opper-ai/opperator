package messages

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// MouseHandler provides mouse coordinate to text position mapping for messages.
type MouseHandler struct {
	// The Y position where the message starts on screen
	messageY int
	// The height of the message
	messageHeight int
	// Content width (accounting for padding and borders)
	contentWidth int
	// Left padding/border offset
	leftOffset int
}

// NewMouseHandler creates a new mouse handler for a message component.
func NewMouseHandler(messageY, messageHeight, contentWidth, leftOffset int) *MouseHandler {
	return &MouseHandler{
		messageY:      messageY,
		messageHeight: messageHeight,
		contentWidth:  contentWidth,
		leftOffset:    leftOffset,
	}
}

// IsMouseInMessage checks if the mouse coordinates are within the message bounds.
func (m *MouseHandler) IsMouseInMessage(mouse tea.Mouse) bool {
	return mouse.Y >= m.messageY &&
		mouse.Y < m.messageY+m.messageHeight
}

// CoordinatesToTextPosition converts screen coordinates to text row/column.
// Returns (-1, -1) if the coordinates are outside the message content.
func (m *MouseHandler) CoordinatesToTextPosition(mouse tea.Mouse, renderedLines []string) (row, col int) {
	// Check Y bounds
	relativeY := mouse.Y - m.messageY
	if relativeY < 0 || relativeY >= len(renderedLines) {
		return -1, -1
	}

	row = relativeY

	// Check X bounds - account for left padding/border
	relativeX := mouse.X - m.leftOffset
	if relativeX < 0 {
		return row, 0
	}

	// Get the line and calculate column based on display width
	if row >= len(renderedLines) {
		return -1, -1
	}

	line := renderedLines[row]
	lineWidth := ansi.StringWidth(stripANSI(line))

	// If click is beyond the line, snap to end of line
	if relativeX >= lineWidth {
		return row, lineWidth
	}

	// Find the column position accounting for rune widths
	col = calculateColumn(stripANSI(line), relativeX)
	return row, col
}

// calculateColumn finds the column index for a given display width position.
// This accounts for double-width runes and complex characters.
func calculateColumn(line string, targetWidth int) int {
	if targetWidth <= 0 {
		return 0
	}

	currentWidth := 0
	for _, r := range line {
		runeWidth := ansi.StringWidth(string(r))
		if currentWidth+runeWidth > targetWidth {
			// Return the current position if adding this rune would exceed target
			return currentWidth
		}
		currentWidth += runeWidth
	}

	return currentWidth
}

// GetRenderedLines splits the rendered content into lines for coordinate mapping.
// This is a helper to prepare content for mouse position calculations.
func GetRenderedLines(renderedContent string) []string {
	if renderedContent == "" {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(renderedContent, "\n"), "\n")
}

// CalculateLeftOffset determines the left offset based on message role and focus state.
// User messages with focus: 2 chars (1 border + 1 padding)
// User messages without focus: 2 chars (1 border + 1 padding)
// Assistant messages with focus: 2 chars (1 border + 1 padding)
// Assistant messages without focus: 2 chars (padding only)
func CalculateLeftOffset(isUser bool, isFocused bool) int {
	// Based on the View() method in message_cmp.go:
	// User messages always have BorderLeft(true) + PaddingLeft(1)
	// Assistant messages have BorderLeft(true) when focused, else PaddingLeft(2)
	if isUser {
		return 2 // border (1) + padding (1)
	}
	if isFocused {
		return 2 // border (1) + padding (1)
	}
	return 2 // padding (2)
}
