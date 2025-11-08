package messages

import (
	"fmt"
	"image/color"
	"strings"
	"time"
	"unicode"

	"tui/components/anim"
	"tui/internal/message"
	"tui/styles"
	"tui/util"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

// spinner + footer timing similar to the prior inline renderer.
type MessageCmp interface {
	tea.Model
	tea.ViewModel
	SetSize(w, h int) tea.Cmd
	Focus() tea.Cmd
	Blur() tea.Cmd
	IsFocused() bool
}

type messageCmp struct {
	width   int
	focused bool

	// core data
	msg        message.Message
	agentID    string
	agentName  string
	agentColor string

	// thinking + timing
	thinking   bool
	startedAt  time.Time
	finishedAt time.Time
	duration   time.Duration

	anim         *anim.Anim
	animSettings anim.Settings

	// selection state
	selection SelectionState
}

// newMessageCmp constructs a message component from role and content.
func newMessageCmp(role message.Role, content string, width int, focused bool) *messageCmp {
	t := styles.CurrentTheme()
	id := uuid.NewString()
	var m message.Message
	switch role {
	case message.User:
		m = message.NewUser(content)
	default:
		m = message.NewAssistant(content)
	}
	m.ID = id

	thinkingTexts := []string{
		"Thinking...",
		"Processing...",
		"Pondering...",
		"Analyzing...",
		"One moment...",
		"Opperating...",
		"Oppering...",
		"Computing…",
		"Putering...",
		"Fetching…",
		"Buffering…",
		"Preparing…",
		"Sorting…",
		"Searching…",
		"On my puter...",
		"Hmmm…",
		"Hold on…",
		"Standby…",
		"Almost…",
		"One sec…",
		"In progress…",
		"Patience...",
	}

	settings := anim.Settings{
		Label:           thinkingTexts[time.Now().UnixNano()%int64(len(thinkingTexts))],
		Size:            20,
		LabelColor:      t.FgBase,
		GradColorA:      t.Primary,
		GradColorB:      t.Secondary,
		CycleColors:     true,
		BuildLabel:      true,
		BuildInterval:   50 * time.Millisecond,
		BuildDelay:      300 * time.Millisecond,
		ShufflePrelude:  1500 * time.Millisecond,
		ShowEllipsis:    false,
		CycleReveal:     true,
		DisplayDuration: 4 * time.Second,
	}

	cmp := &messageCmp{
		msg:          m,
		anim:         anim.New(settings),
		animSettings: settings,
	}
	cmp.SetSize(width, 0)
	if focused {
		cmp.Focus()
	}
	return cmp
}

func (m *messageCmp) role() message.Role { return m.msg.Role }
func (m *messageCmp) content() string    { return m.msg.Content().String() }
func (m *messageCmp) setContent(s string) {
	replaced := false
	for i, p := range m.msg.Parts {
		if _, ok := p.(message.TextContent); ok {
			m.msg.Parts[i] = message.TextContent{Text: s}
			replaced = true
			break
		}
	}
	if !replaced {
		m.msg.Parts = append(m.msg.Parts, message.TextContent{Text: s})
	}
}
func (m *messageCmp) appendContent(delta string) { m.setContent(m.content() + delta) }

// Public hooks used by Messages
func (m *messageCmp) markStarted() {
	if m.startedAt.IsZero() {
		m.startedAt = time.Now()
	}
	m.duration = 0
}
func (m *messageCmp) markFinished() {
	if m.finishedAt.IsZero() {
		m.finishedAt = time.Now()
	}
	if !m.startedAt.IsZero() {
		delta := m.finishedAt.Sub(m.startedAt)
		if delta < 0 {
			delta = 0
		}
		m.duration = delta
	}
}
func (m *messageCmp) setThinking(v bool) {
	if m.thinking == v {
		return
	}
	m.thinking = v
	if v {
		m.anim = anim.New(m.animSettings)
	}
}
func (m *messageCmp) isAssistant() bool { return m.msg.Role == message.Assistant }

func (m *messageCmp) setAgentInfo(id, name, color string) bool {
	trimmedID := strings.TrimSpace(id)
	trimmedName := strings.TrimSpace(name)
	trimmedColor := strings.TrimSpace(color)
	changed := false
	if m.agentID != trimmedID {
		m.agentID = trimmedID
		changed = true
	}
	if m.agentName != trimmedName {
		m.agentName = trimmedName
		changed = true
	}
	if m.agentColor != trimmedColor {
		m.agentColor = trimmedColor
		changed = true
	}
	return changed
}

func (m *messageCmp) ensureAgentDefaults(id, name, color string) bool {
	if strings.TrimSpace(m.agentID) != "" {
		return false
	}
	return m.setAgentInfo(id, name, color)
}

func (m *messageCmp) agentMatches(id string) bool {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(m.agentID), trimmed)
}

func (m *messageCmp) setDuration(d time.Duration) bool {
	if d < 0 {
		d = 0
	}
	if m.duration == d {
		return false
	}
	m.duration = d
	return true
}

func (m *messageCmp) applyTurnSummary(id, name, color string, d time.Duration) bool {
	changed := m.setAgentInfo(id, name, color)
	if d > 0 && m.setDuration(d) {
		changed = true
	}
	return changed
}

// tea.Model
func (m *messageCmp) Init() tea.Cmd { return m.anim.Init() }

func (m *messageCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case anim.StepMsg:
		// keep spinner running only if thinking
		if m.thinking && m.isAssistant() {
			u, cmd := m.anim.Update(msg)
			if a, ok := u.(*anim.Anim); ok {
				m.anim = a
			}
			return m, cmd
		}
	case tea.KeyPressMsg:
		// Handle ESC to clear selection
		if m.focused && key.Matches(msg, ClearSelectionKey) {
			if m.selection.HasSelection() {
				m.selection.ClearSelection()
				return m, nil
			}
		}

		// Only the focused message should react to copy commands
		if m.focused && key.Matches(msg, CopyKey) {
			var content string
			if m.selection.HasSelection() {
				// Copy selected text
				rendered := renderMarkdown(max(m.width-2, 1), m.content())
				lines := GetRenderedLines(rendered)
				content = m.selection.GetSelectedText(lines)
			} else {
				// Fallback to full message content
				content = m.content()
			}
			return m, tea.Sequence(
				tea.SetClipboard(content),
				func() tea.Msg {
					_ = clipboard.WriteAll(content)
					return nil
				},
				util.ReportInfo("Text copied to clipboard"),
			)
		}
	case tea.MouseClickMsg:
		// Only handle mouse events when focused
		if !m.focused {
			return m, nil
		}

		// TODO: This requires the message Y position and height to be passed in
		// For now, we'll handle mouse events at the messages container level
		// and pass the relative coordinates here
		return m, nil

	case tea.MouseMotionMsg:
		// Only handle when focused and dragging a selection
		if !m.focused || !m.selection.isDragging {
			return m, nil
		}

		// TODO: Update selection end position
		// Will be implemented when container forwards events
		return m, nil

	case tea.MouseReleaseMsg:
		// Finalize selection on mouse release
		if m.selection.isDragging {
			m.selection.EndDragging()
		}
		return m, nil
	}
	return m, nil
}

func (m *messageCmp) View() string {
	t := styles.CurrentTheme()
	style := t.S().Text
	leftOnly := lipgloss.Border{Left: "▌"}

	switch m.msg.Role {
	case message.User:
		if m.focused {
			style = style.PaddingLeft(1).BorderLeft(true).BorderStyle(leftOnly).BorderForeground(t.Secondary)
		} else {
			style = style.PaddingLeft(1).BorderLeft(true).BorderStyle(leftOnly).BorderForeground(t.Primary)
		}
	case message.Assistant:
		if m.focused {
			style = style.PaddingLeft(1).BorderLeft(true).BorderStyle(leftOnly).BorderForeground(t.Secondary)
		} else {
			style = style.PaddingLeft(2)
		}
	}

	var parts []string

	// Render content first (so it appears at the top)
	content := strings.TrimSuffix(m.content(), "\n")
	if content != "" {
		const cancelSuffix = "Request has been cancelled."
		if strings.HasSuffix(content, cancelSuffix) {
			base := strings.TrimSuffix(content, cancelSuffix)
			var rendered string
			if strings.TrimSpace(base) != "" {
				rendered = renderMarkdown(max(m.width-2, 1), base)
				// Apply selection highlighting if active
				rendered = m.applySelectionHighlighting(rendered)
			}
			ttheme := styles.CurrentTheme()
			suffixStyle := ttheme.S().Muted.Copy().Foreground(ttheme.Error)
			muted := suffixStyle.Render(cancelSuffix)
			if rendered == "" {
				parts = append(parts, muted)
			} else {
				parts = append(parts, rendered+muted)
			}
		} else {
			rendered := renderMarkdown(max(m.width-2, 1), content)
			// Apply selection highlighting if active
			rendered = m.applySelectionHighlighting(rendered)
			parts = append(parts, rendered)
		}
	}

	// Render spinner second (so it appears below content)
	if m.isAssistant() && m.thinking {
		// Constrain spinner width to available message width (accounting for padding/borders)
		spinnerView := m.anim.View()
		maxSpinnerWidth := max(m.width-4, 10) // Reserve space for borders/padding, minimum 10
		if lipgloss.Width(spinnerView) > maxSpinnerWidth {
			spinnerView = lipgloss.NewStyle().MaxWidth(maxSpinnerWidth).Render(spinnerView)
		}
		parts = append(parts, t.S().Base.PaddingLeft(0).Render(spinnerView))
	}

	// Render footer last (so it appears at the bottom when not thinking)
	if m.isAssistant() && !m.thinking {
		d := m.duration
		if d <= 0 && !m.finishedAt.IsZero() && !m.startedAt.IsZero() {
			delta := m.finishedAt.Sub(m.startedAt)
			if delta > 0 {
				d = delta
			}
		}
		duration := formatDuration(d)
		if duration != "" {
			name := strings.TrimSpace(m.agentName)
			if name == "" {
				name = "Opperator"
			}
			// agentStyle := t.S().Subtle.Copy().Bold(false).Foreground(agentColorOrSecondary(m.agentColor, t.Secondary))
			agentStyle := t.S().Subtle.Copy().Bold(false)
			footer := agentStyle.Render("∴ " + name)
			footer += t.S().Subtle.Render(" • " + duration)
			parts = append(parts, footer)
		}
	}

	return style.Width(max(m.width, 1)).Render(strings.Join(parts, "\n\n"))
}

// Focusable
func (m *messageCmp) Focus() tea.Cmd { m.focused = true; return nil }
func (m *messageCmp) Blur() tea.Cmd {
	m.focused = false
	m.selection.ClearSelection() // Clear selection when losing focus
	return nil
}
func (m *messageCmp) IsFocused() bool { return m.focused }

// Sizeable
func (m *messageCmp) SetSize(w, _ int) tea.Cmd { m.width = w; return nil }

// ClearSelection clears any active text selection in this message.
func (m *messageCmp) ClearSelection() {
	m.selection.ClearSelection()
}

// SelectedText returns the currently selected text, if any.
func (m *messageCmp) SelectedText() string {
	if !m.selection.HasSelection() {
		return ""
	}
	rendered := renderMarkdown(max(m.width-2, 1), m.content())
	lines := GetRenderedLines(rendered)
	return m.selection.GetSelectedText(lines)
}

// HandleMouseEvent processes mouse events for text selection.
// messageY is the Y position of the message on screen.
// Returns a command if any action needs to be taken (e.g., copying on double-click).
func (m *messageCmp) HandleMouseEvent(msg tea.Msg, messageY int) tea.Cmd {
	logToFile("HandleMouseEvent called: focused=%v messageY=%d\n", m.focused, messageY)

	// Allow selection even without focus (user preference)
	// if !m.focused {
	// 	logToFile("Message not focused, ignoring\n")
	// 	return nil
	// }

	// Get rendered content lines for coordinate mapping
	// These are ONLY the content lines, not spinner or footer
	rendered := renderMarkdown(max(m.width-2, 1), m.content())
	lines := GetRenderedLines(rendered)
	if len(lines) == 0 {
		return nil
	}

	contentOffset := m.contentStartOffset(len(lines) > 0)
	contentTop := messageY + contentOffset

	logToFile("Content has %d lines (contentTop=%d to %d)\n",
		len(lines), contentTop, contentTop+len(lines)-1)

	// Calculate left offset based on role and focus
	leftOffset := CalculateLeftOffset(m.msg.Role == message.User, m.focused)
	logToFile("Left offset: %d\n", leftOffset)

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button != tea.MouseLeft {
			return nil
		}
		logToFile("Click at screen: x=%d y=%d, messageY=%d contentTop=%d\n",
			mouse.X, mouse.Y, messageY, contentTop)

		// Convert screen coordinates to text position
		// The relativeY is relative to messageY (start of the whole message block)
		relativeY := mouse.Y - contentTop
		logToFile("Relative Y (content): %d (content lines: %d)\n", relativeY, len(lines))

		// Check if click is within the content lines (0 to len(lines)-1)
		// Content starts at line 0 of the message
		if relativeY < 0 || relativeY >= len(lines) {
			// Click outside content bounds - clear selection
			logToFile("Click outside content bounds (relativeY=%d not in 0-%d), clearing selection\n", relativeY, len(lines)-1)
			m.selection.ClearSelection()
			return nil
		}

		row := relativeY
		relativeX := mouse.X - leftOffset
		if relativeX < 0 {
			relativeX = 0
		}

		plainLine := stripANSI(lines[row])
		col := calculateColumn(plainLine, relativeX)
		logToFile("Starting selection at row=%d col=%d\n", row, col)

		now := time.Now()
		clickCount := m.selection.RecordClick(mouse.X, mouse.Y, now)

		switch clickCount {
		case 3:
			lineWidth := ansi.StringWidth(plainLine)
			m.selection.StartSelection(row, 0, selectionModeLine)
			m.selection.UpdateSelection(row, lineWidth)
			m.selection.anchorEnd = lineWidth
			return nil
		case 2:
			startWidth, endWidth := findWordBounds(plainLine, col)
			if endWidth > startWidth {
				m.selection.StartSelection(row, startWidth, selectionModeWord)
				m.selection.UpdateSelection(row, endWidth)
				m.selection.anchorEnd = endWidth
			} else {
				m.selection.ClearSelection()
			}
			return nil
		default:
			m.selection.StartSelection(row, col, selectionModeChar)
			return nil
		}

	case tea.MouseMotionMsg:
		if !m.selection.isDragging {
			return nil
		}

		mouse := msg.Mouse()
		if mouse.Button != tea.MouseLeft {
			return nil
		}
		relativeY := mouse.Y - contentTop

		// Clamp to content bounds (0 to len(lines)-1)
		if relativeY < 0 {
			relativeY = 0
		}
		if relativeY >= len(lines) {
			relativeY = len(lines) - 1
		}

		row := relativeY
		relativeX := mouse.X - leftOffset
		if relativeX < 0 {
			relativeX = 0
		}

		plainLine := stripANSI(lines[row])
		col := calculateColumn(plainLine, relativeX)

		switch m.selection.mode {
		case selectionModeWord:
			ptrStart, ptrEnd := findWordBounds(plainLine, col)
			anchorRow := m.selection.anchorRow
			anchorStart := m.selection.anchorStart
			anchorEnd := m.selection.anchorEnd

			if row == anchorRow && ptrStart <= anchorEnd && ptrEnd >= anchorStart {
				return nil
			}

			if row < anchorRow || (row == anchorRow && ptrStart < anchorStart) {
				m.selection.startRow = row
				m.selection.startCol = ptrStart
				m.selection.endRow = anchorRow
				m.selection.endCol = anchorEnd
			} else {
				m.selection.startRow = anchorRow
				m.selection.startCol = anchorStart
				m.selection.endRow = row
				m.selection.endCol = ptrEnd
			}
			return nil

		case selectionModeLine:
			anchorRow := m.selection.anchorRow
			anchorEnd := m.selection.anchorEnd
			lineWidth := ansi.StringWidth(plainLine)

			if row == anchorRow {
				return nil
			}

			if row < anchorRow {
				m.selection.startRow = row
				m.selection.startCol = 0
				m.selection.endRow = anchorRow
				m.selection.endCol = anchorEnd
			} else {
				m.selection.startRow = anchorRow
				m.selection.startCol = 0
				m.selection.endRow = row
				m.selection.endCol = lineWidth
			}
			return nil

		default:
			m.selection.UpdateSelection(row, col)
			return nil
		}

	case tea.MouseReleaseMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft && m.selection.isDragging {
			m.selection.EndDragging()
		}
		return nil
	}

	return nil
}

func (m *messageCmp) contentStartOffset(hasContent bool) int {
	// Content is now always at the top, so offset is always 0
	// (spinner renders below content instead of above)
	return 0
}

func findWordBounds(line string, col int) (int, int) {
	if line == "" {
		return 0, 0
	}

	runes := []rune(line)
	type runeInfo struct {
		start int
		width int
	}
	infos := make([]runeInfo, len(runes))

	cumulative := 0
	for i, r := range runes {
		width := ansi.StringWidth(string(r))
		if width < 1 {
			width = 1
		}
		infos[i] = runeInfo{start: cumulative, width: width}
		cumulative += width
	}

	totalWidth := cumulative
	if totalWidth <= 0 {
		return 0, 0
	}

	if col < 0 {
		col = 0
	}
	if col >= totalWidth {
		col = totalWidth - 1
	}

	targetIdx := len(runes) - 1
	for i := 0; i < len(runes); i++ {
		start := infos[i].start
		end := start + infos[i].width
		if col < end {
			targetIdx = i
			break
		}
	}

	category := classifyRune(runes[targetIdx])

	startIdx := targetIdx
	for startIdx > 0 {
		prev := startIdx - 1
		if classifyRune(runes[prev]) != category {
			break
		}
		startIdx--
	}

	endIdx := targetIdx
	for endIdx+1 < len(runes) {
		next := endIdx + 1
		if classifyRune(runes[next]) != category {
			break
		}
		endIdx++
	}

	startWidth := infos[startIdx].start
	endWidth := infos[endIdx].start + infos[endIdx].width

	return startWidth, endWidth
}

type runeCategory int

const (
	categoryWhitespace runeCategory = iota
	categoryWord
	categoryOther
)

func classifyRune(r rune) runeCategory {
	switch {
	case unicode.IsSpace(r):
		return categoryWhitespace
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-':
		return categoryWord
	default:
		return categoryOther
	}
}

func agentColorOrSecondary(raw string, fallback color.Color) color.Color {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return colorToLipgloss(fallback)
	}
	return lipgloss.Color(trimmed)
}

func colorToLipgloss(c color.Color) color.Color {
	if rgba, ok := c.(color.RGBA); ok {
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", rgba.R, rgba.G, rgba.B))
	}
	r, g, b, _ := c.RGBA()
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8)))
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	trunc := d.Truncate(100 * time.Millisecond)
	if trunc <= 0 {
		trunc = d
	}
	return trunc.String()
}

// applySelectionHighlighting applies visual highlighting to selected text ranges.
func (m *messageCmp) applySelectionHighlighting(rendered string) string {
	if !m.selection.HasSelection() {
		return rendered
	}

	selectionStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("240")).
		Foreground(lipgloss.Color("15"))

	lines := GetRenderedLines(rendered)
	startRow, startCol, endRow, endCol := m.selection.GetNormalizedBounds()

	// Bounds check
	if startRow < 0 || startRow >= len(lines) {
		return rendered
	}
	if endRow < 0 || endRow >= len(lines) {
		return rendered
	}

	// Apply highlighting to each line in the selection
	for i := startRow; i <= endRow && i < len(lines); i++ {
		line := lines[i]
		lineStart := 0
		lineEnd := ansi.StringWidth(stripANSI(line))

		// Determine the range to highlight on this line
		if i == startRow {
			lineStart = startCol
		}
		if i == endRow {
			lineEnd = endCol
		}

		// Skip if invalid range
		if lineStart >= lineEnd || lineStart >= ansi.StringWidth(stripANSI(line)) {
			continue
		}

		// Create a style range for this line
		if lineEnd > lineStart {
			lines[i] = lipgloss.StyleRanges(line,
				lipgloss.NewRange(lineStart, lineEnd, selectionStyle),
			)
		}
	}

	// Rejoin the lines
	return strings.Join(lines, "\n")
}
