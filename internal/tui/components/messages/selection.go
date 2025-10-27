package messages

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const (
	doubleClickThreshold = 500 * time.Millisecond
	clickTolerance       = 2 // pixels
)

// SelectionState tracks the text selection state for a message component.
type SelectionState struct {
	active         bool
	startRow       int
	startCol       int
	endRow         int
	endCol         int
	isDragging     bool
	lastClickTime  time.Time
	lastClickX     int
	lastClickY     int
	lastClickCount int
	mode           selectionMode
	anchorRow      int
	anchorStart    int
	anchorEnd      int
}

type selectionMode int

const (
	selectionModeChar selectionMode = iota
	selectionModeWord
	selectionModeLine
)

// HasSelection returns true if there is an active selection.
func (s *SelectionState) HasSelection() bool {
	return s.active
}

// StartSelection begins a new selection at the given row and column.
func (s *SelectionState) StartSelection(row, col int, mode selectionMode) {
	s.active = true
	s.startRow = row
	s.startCol = col
	s.endRow = row
	s.endCol = col
	s.isDragging = true
	s.mode = mode
	s.anchorRow = row
	s.anchorStart = col
	s.anchorEnd = col
}

// UpdateSelection updates the end position of the selection.
func (s *SelectionState) UpdateSelection(row, col int) {
	if !s.active {
		return
	}
	s.endRow = row
	s.endCol = col
}

// EndDragging marks the end of a drag operation.
func (s *SelectionState) EndDragging() {
	s.isDragging = false
}

// ClearSelection clears the current selection.
func (s *SelectionState) ClearSelection() {
	s.active = false
	s.startRow = 0
	s.startCol = 0
	s.endRow = 0
	s.endCol = 0
	s.isDragging = false
	s.mode = selectionModeChar
	s.anchorRow = 0
	s.anchorStart = 0
	s.anchorEnd = 0
}

// GetNormalizedBounds returns the selection bounds in normalized form
// (start always before end), handling cases where user drags backwards.
func (s *SelectionState) GetNormalizedBounds() (startRow, startCol, endRow, endCol int) {
	if !s.active {
		return 0, 0, 0, 0
	}

	// If selection is on same row
	if s.startRow == s.endRow {
		if s.startCol <= s.endCol {
			return s.startRow, s.startCol, s.endRow, s.endCol
		}
		return s.startRow, s.endCol, s.endRow, s.startCol
	}

	// If selection spans multiple rows
	if s.startRow < s.endRow {
		return s.startRow, s.startCol, s.endRow, s.endCol
	}
	return s.endRow, s.endCol, s.startRow, s.startCol
}

// GetSelectedText extracts the selected text from the given lines.
// Returns plain text without ANSI codes or styling.
func (s *SelectionState) GetSelectedText(lines []string) string {
	if !s.active || len(lines) == 0 {
		return ""
	}

	startRow, startCol, endRow, endCol := s.GetNormalizedBounds()

	// Bounds check
	if startRow < 0 || startRow >= len(lines) {
		return ""
	}
	if endRow < 0 || endRow >= len(lines) {
		return ""
	}

	// Single line selection
	if startRow == endRow {
		line := stripANSI(lines[startRow])
		lineWidth := ansi.StringWidth(line)
		if startCol >= lineWidth {
			return ""
		}
		endCol = min(endCol, lineWidth)
		return substringByWidth(line, startCol, endCol)
	}

	// Multi-line selection
	var result strings.Builder

	// First line (from startCol to end)
	firstLine := stripANSI(lines[startRow])
	firstLineWidth := ansi.StringWidth(firstLine)
	if startCol < firstLineWidth {
		result.WriteString(substringByWidth(firstLine, startCol, firstLineWidth))
	}

	// Middle lines (entire lines)
	for i := startRow + 1; i < endRow; i++ {
		result.WriteString("\n")
		result.WriteString(stripANSI(lines[i]))
	}

	// Last line (from start to endCol)
	if endRow > startRow {
		result.WriteString("\n")
		lastLine := stripANSI(lines[endRow])
		lastLineWidth := ansi.StringWidth(lastLine)
		endCol = min(endCol, lastLineWidth)
		if endCol > 0 {
			result.WriteString(substringByWidth(lastLine, 0, endCol))
		}
	}

	return result.String()
}

// RecordClick records the position and time of a click and returns the click count
// (1 for single, 2 for double, 3 for triple within the detection window).
func (s *SelectionState) RecordClick(x, y int, now time.Time) int {
	if s.lastClickTime.IsZero() {
		s.lastClickCount = 1
	} else {
		timeDiff := now.Sub(s.lastClickTime)
		xDiff := abs(x - s.lastClickX)
		yDiff := abs(y - s.lastClickY)
		if timeDiff <= doubleClickThreshold &&
			xDiff <= clickTolerance &&
			yDiff <= clickTolerance {
			s.lastClickCount++
			if s.lastClickCount > 3 {
				s.lastClickCount = 1
			}
		} else {
			s.lastClickCount = 1
		}
	}

	s.lastClickX = x
	s.lastClickY = y
	s.lastClickTime = now
	return s.lastClickCount
}

// stripANSI removes ANSI escape codes from a string.
func stripANSI(s string) string {
	return ansi.Strip(s)
}

// substringByWidth extracts a substring based on display width (accounting for runes).
// Returns the substring from startWidth to endWidth display positions.
func substringByWidth(s string, startWidth, endWidth int) string {
	if startWidth >= endWidth {
		return ""
	}

	var result strings.Builder
	currentWidth := 0

	for _, r := range s {
		runeWidth := ansi.StringWidth(string(r))

		// Skip runes before start position
		if currentWidth+runeWidth <= startWidth {
			currentWidth += runeWidth
			continue
		}

		// Stop if we've reached the end position
		if currentWidth >= endWidth {
			break
		}

		result.WriteRune(r)
		currentWidth += runeWidth
	}

	return result.String()
}

// abs returns the absolute value of an integer.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
