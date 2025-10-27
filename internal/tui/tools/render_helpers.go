package tools

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tui/ansiext"
	"tui/highlight"
	"tui/styles"
)

const defaultPreviewLines = 10

func renderCodePreview(filePath, content string, offset, width, maxLines int) string {
	return strings.Join(codePreviewLines(filePath, content, offset, width, maxLines), "\n")
}

func codePreviewLines(filePath, content string, offset, width, maxLines int) []string {
	t := styles.CurrentTheme()
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\t", "    ")

	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	limit := maxLines
	if limit <= 0 {
		limit = defaultPreviewLines
	}

	display := lines
	truncatedCount := 0
	if len(lines) > limit {
		display = lines[:limit]
		truncatedCount = len(lines) - limit
	}

	for i := range display {
		display[i] = ansiext.Escape(display[i])
	}

	joined := strings.Join(display, "\n")
	if highlighted, err := highlight.SyntaxHighlight(joined, filePath, t.BgBase); err == nil && highlighted != "" {
		display = strings.Split(strings.TrimRight(highlighted, "\n"), "\n")
	} else {
		display = strings.Split(joined, "\n")
	}

	gutter := lipgloss.NewStyle().MarginLeft(2).Foreground(t.FgMuted).Render("│") + " "
	numberStyle := t.S().Base.Foreground(t.FgMuted)
	codeStyle := t.S().Base.Foreground(t.FgBase)

	maxDigits := digits(offset + len(display))
	available := intMax(width-lipgloss.Width(gutter)-maxDigits-2, 16)
	if width <= 0 {
		available = 16
	}

	result := make([]string, 0, len(display)+2)
	for i, line := range display {
		lineNumber := fmt.Sprintf("%*d", maxDigits, offset+i+1)
		trimmed := ansi.Truncate(line, available, "…")
		result = append(result, fmt.Sprintf("%s%s %s",
			gutter,
			numberStyle.Render(lineNumber),
			codeStyle.Render(trimmed),
		))
	}

	if truncatedCount > 0 {
		placeholder := strings.Repeat(" ", maxDigits)
		message := fmt.Sprintf("… (%d more lines)", truncatedCount)
		result = append(result, fmt.Sprintf("%s%s %s",
			gutter,
			numberStyle.Render(placeholder),
			t.S().Muted.Render(message),
		))
	}

	return result
}

func digits(n int) int {
	if n <= 0 {
		return 1
	}
	count := 0
	for n > 0 {
		n /= 10
		count++
	}
	if count < 1 {
		return 1
	}
	return count
}

func gutterLabel(text string) string {
	t := styles.CurrentTheme()
	gutter := lipgloss.NewStyle().MarginLeft(2).Foreground(t.FgMuted).Render("│ ")
	return gutter + t.S().Muted.Render(text)
}

func renderGutterList(lines []string, width int, transform func(string) string) string {
	if len(lines) == 0 {
		return ""
	}

	t := styles.CurrentTheme()
	gutter := lipgloss.NewStyle().MarginLeft(2).Foreground(t.FgMuted).Render("│ ")
	gutterWidth := lipgloss.Width(gutter)

	contentWidth := width
	if contentWidth <= 0 {
		contentWidth = 80
	}
	minWidth := gutterWidth + 8
	if contentWidth < minWidth {
		contentWidth = minWidth
	}
	maxLineWidth := contentWidth - gutterWidth

	if transform == nil {
		baseStyle := t.S().Base.Foreground(t.FgBase)
		transform = func(s string) string {
			return baseStyle.Render(s)
		}
	}

	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		collapsed := strings.ReplaceAll(line, "\n", " ")
		collapsed = strings.ReplaceAll(collapsed, "\r", " ")
		collapsed = strings.ReplaceAll(collapsed, "\t", " ")

		for strings.Contains(collapsed, "  ") {
			collapsed = strings.ReplaceAll(collapsed, "  ", " ")
		}

		trimmed := strings.TrimSpace(collapsed)
		styled := transform(trimmed)
		b.WriteString(gutter)
		b.WriteString(truncateWidth(styled, maxLineWidth))
	}

	return b.String()
}

func shortenText(s string, limit int) string {
	s = strings.TrimSpace(s)
	if limit <= 0 || lipgloss.Width(s) <= limit {
		return s
	}
	rs := []rune(s)
	if len(rs) == 0 {
		return s
	}
	if limit <= 1 {
		return string(rs[:1])
	}
	max := limit - 1
	for len(rs) > 0 && lipgloss.Width(string(rs)) > max {
		rs = rs[:len(rs)-1]
	}
	if len(rs) == 0 {
		return "…"
	}
	return string(rs) + "…"
}

func intMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func truncateWidth(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	if lipgloss.Width(s) <= limit {
		return s
	}
	rs := []rune(s)
	lo, hi := 0, len(rs)
	for lo < hi {
		mid := (lo + hi) / 2
		if lipgloss.Width(string(rs[:mid])) <= limit-1 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	cut := lo - 1
	if cut < 0 {
		cut = 0
	}
	return string(rs[:cut]) + "…"
}
