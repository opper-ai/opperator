package tools

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/lipgloss/v2"

	"tui/ansiext"
	"tui/styles"
	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
)

func init() {
	toolregistry.Register(EditToolName, toolregistry.Definition{
		Label: "Edit",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			var params EditParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			label := "└ Edit"
			if base := filepath.Base(strings.TrimSpace(params.FilePath)); base != "" && base != "." && base != "/" {
				label = fmt.Sprintf("└ Edit %s", base)
			}
			return strings.TrimSpace(label + " " + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params EditParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta EditMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			filePath := strings.TrimSpace(params.FilePath)
			extra := []string{}
			fallback := strings.TrimRight(result.Content, "\n")

			return renderEditView(
				"Edit",
				filePath,
				meta.Additions,
				meta.Removals,
				extra,
				meta.Diff,
				fallback,
				width,
				result.IsError,
			)
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params EditParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			base := filepath.Base(strings.TrimSpace(params.FilePath))
			if base == "" || base == "." || base == "/" {
				return "Edit"
			}
			return "Edit " + base
		},
	})

	multiEditDefinition := toolregistry.Definition{
		Label: "Multi-Edit",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			var params MultiEditParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			label := "└ Multi-edit"
			if base := filepath.Base(strings.TrimSpace(params.FilePath)); base != "" && base != "." && base != "/" {
				label = fmt.Sprintf("└ Multi-edit %s", base)
			}
			if n := len(params.Edits); n > 0 {
				label = fmt.Sprintf("%s (%d edits)", label, n)
			}
			return strings.TrimSpace(label + " " + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params MultiEditParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta MultiEditMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			extra := []string{}
			if n := len(params.Edits); n > 0 {
				extra = append(extra, fmt.Sprintf("%d edits", n))
			}

			return renderEditView(
				"Multi-edit",
				strings.TrimSpace(params.FilePath),
				meta.Additions,
				meta.Removals,
				extra,
				meta.Diff,
				strings.TrimRight(result.Content, "\n"),
				width,
				result.IsError,
			)
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params MultiEditParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			base := filepath.Base(strings.TrimSpace(params.FilePath))
			if base == "" || base == "." || base == "/" {
				return "Multi-edit"
			}
			return "Multi-edit " + base
		},
	}

	toolregistry.Register(MultiEditToolName, multiEditDefinition)
	toolregistry.Register("multi_edit", multiEditDefinition)
}

func renderEditView(label, filePath string, additions, removals int, extras []string, diff, fallback string, width int, isError bool) string {
	t := styles.CurrentTheme()
	header := label
	if base := filepath.Base(filePath); base != "" && base != "." && base != "/" {
		header = fmt.Sprintf("%s %s", header, base)
	}
	if additions > 0 || removals > 0 {
		header = fmt.Sprintf("%s (+%d -%d)", header, additions, removals)
	}
	if len(extras) > 0 {
		header = fmt.Sprintf("%s (%s)", header, strings.Join(extras, ", "))
	}
	headerView := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + header)

	if isError {
		errorMsg := lipgloss.NewStyle().Foreground(t.Error).Render(strings.TrimSpace(fallback))
		return headerView + "\n\n" + errorMsg
	}

	body := buildEditBody(filePath, width, additions, removals, diff, fallback)
	return headerView + "\n\n" + body
}

func buildEditBody(filePath string, width, additions, removals int, diff, fallback string) string {
	sections := make([]string, 0, 2)

	// If we have a diff string from metadata, parse and render it
	if strings.TrimSpace(diff) != "" {
		if diffBody, ok := renderDiffFromString(filePath, width, diff, defaultPreviewLines); ok {
			sections = append(sections, diffBody)
		}
	}

	if len(sections) == 0 {
		trimmed := strings.TrimSpace(fallback)
		if trimmed != "" {
			sections = append(sections, gutterLabel("Result")+"\n"+renderCodePreview(filePath, strings.TrimRight(fallback, "\n"), 0, width, defaultPreviewLines))
		} else {
			sections = append(sections, gutterLabel("No preview available"))
		}
	}
	return strings.Join(sections, "\n"+gutterLabel("\n"))
}

func renderDiffFromString(filePath string, width int, diffString string, maxLines int) (string, bool) {
	if strings.TrimSpace(diffString) == "" {
		return "", false
	}

	// The diff string is already a unified diff format
	// Parse it line by line and render with colors
	lines := strings.Split(diffString, "\n")
	if len(lines) == 0 {
		return "", false
	}

	t := styles.CurrentTheme()
	headerStyle := t.S().Muted
	contextStyle := t.S().Base.Foreground(t.FgMuted)
	insertStyle := t.S().Base.Foreground(t.Green)
	deleteStyle := t.S().Base.Foreground(t.Red)

	renderedLines := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			renderedLines = append(renderedLines, headerStyle.Render(line))
		} else if strings.HasPrefix(line, "@@") {
			renderedLines = append(renderedLines, headerStyle.Render(line))
		} else if strings.HasPrefix(line, "+") {
			renderedLines = append(renderedLines, insertStyle.Render(line))
		} else if strings.HasPrefix(line, "-") {
			renderedLines = append(renderedLines, deleteStyle.Render(line))
		} else {
			renderedLines = append(renderedLines, contextStyle.Render(line))
		}
	}

	if maxLines > 0 && len(renderedLines) > maxLines {
		remaining := len(renderedLines) - maxLines
		truncLine := t.S().Muted.Render(fmt.Sprintf("… (%d more lines)", remaining))
		renderedLines = append(renderedLines[:maxLines], truncLine)
	}

	return renderGutterList(renderedLines, width, func(s string) string { return s }), true
}

func renderDiffSummary(filePath string, width int, oldContent, newContent string, maxLines int) (string, bool) {
	cleanOld := normalizeDiffContent(oldContent)
	cleanNew := normalizeDiffContent(newContent)
	if cleanOld == cleanNew {
		return "", false
	}

	edits := udiff.Strings(cleanOld, cleanNew)
	if len(edits) == 0 {
		return "", false
	}

	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		trimmedPath = "file"
	}
	trimmedPath = filepath.ToSlash(trimmedPath)
	trimmedPath = strings.TrimPrefix(trimmedPath, "/")

	unified, err := udiff.ToUnifiedDiff("a/"+trimmedPath, "b/"+trimmedPath, cleanOld, edits, 2)
	if err != nil || len(unified.Hunks) == 0 {
		return "", false
	}

	body := renderUnifiedDiffPreview(unified, width, maxLines)
	if strings.TrimSpace(body) == "" {
		return "", false
	}

	return body, true
}

func diffHunkLineCounts(h *udiff.Hunk) (before, after int) {
	for _, l := range h.Lines {
		switch l.Kind {
		case udiff.Delete:
			before++
		case udiff.Insert:
			after++
		default:
			before++
			after++
		}
	}
	return
}

func normalizeDiffContent(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\t", "    ")
	return s
}

func renderUnifiedDiffPreview(unified udiff.UnifiedDiff, width, maxLines int) string {
	if maxLines <= 0 {
		maxLines = defaultPreviewLines
	}

	t := styles.CurrentTheme()
	headerStyle := t.S().Muted
	contextStyle := t.S().Base.Foreground(t.FgMuted)
	insertStyle := t.S().Base.Foreground(t.Green)
	deleteStyle := t.S().Base.Foreground(t.Red)
	beforeDigits, afterDigits := diffLineNumberDigits(unified.Hunks)
	if beforeDigits < 1 {
		beforeDigits = 1
	}
	if afterDigits < 1 {
		afterDigits = 1
	}
	padNumber := func(n int, width int) string {
		if n <= 0 {
			return strings.Repeat(" ", width)
		}
		return fmt.Sprintf("%*d", width, n)
	}

	blankBefore := strings.Repeat(" ", beforeDigits)
	blankAfter := strings.Repeat(" ", afterDigits)
	beforeNumberStyle := t.S().Base.Foreground(t.FgMutedMore)
	afterNumberStyle := t.S().Base.Foreground(t.FgMuted)

	lines := make([]string, 0, len(unified.Hunks)*4)

	for _, h := range unified.Hunks {
		beforeCount, afterCount := diffHunkLineCounts(h)
		header := fmt.Sprintf("Diff @@ -%d,%d +%d,%d @@", h.FromLine, beforeCount, h.ToLine, afterCount)
		headerLine := afterNumberStyle.Render(blankAfter) + " " + headerStyle.Render(header)
		lines = append(lines, headerLine)

		beforeLine := h.FromLine
		afterLine := h.ToLine

		for _, l := range h.Lines {
			content := strings.TrimSuffix(l.Content, "\n")
			content = strings.TrimSuffix(content, "\r")
			content = ansiext.Escape(content)

			beforeText := blankBefore
			afterText := blankAfter
			beforeStyle := beforeNumberStyle
			afterStyle := afterNumberStyle
			marker := " "
			style := contextStyle

			switch l.Kind {
			case udiff.Delete:
				beforeText = padNumber(beforeLine, beforeDigits)
				afterText = blankAfter
				afterStyle = afterStyle.Foreground(t.FgMutedMore)
				marker = "-"
				style = deleteStyle
				beforeLine++
			case udiff.Insert:
				beforeText = blankBefore
				afterText = padNumber(afterLine, afterDigits)
				beforeStyle = beforeStyle.Foreground(t.FgMutedMore)
				marker = "+"
				style = insertStyle
				afterLine++
			default:
				beforeText = padNumber(beforeLine, beforeDigits)
				afterText = padNumber(afterLine, afterDigits)
				marker = " "
				style = contextStyle
				beforeLine++
				afterLine++
			}

			line := beforeStyle.Render(beforeText) + " " +
				afterStyle.Render(afterText) + " " +
				style.Render(marker+" "+content)
			lines = append(lines, line)
		}
	}

	if len(lines) == 0 {
		return ""
	}

	if len(lines) > maxLines {
		remaining := len(lines) - maxLines
		truncLine := beforeNumberStyle.Render(blankBefore) + " " + afterNumberStyle.Render(blankAfter) + " " + t.S().Muted.Render(fmt.Sprintf("… (%d more lines)", remaining))
		lines = append(lines[:maxLines], truncLine)
	}

	return renderGutterList(lines, width, func(s string) string { return s })
}

func diffLineNumberDigits(hunks []*udiff.Hunk) (beforeDigits, afterDigits int) {
	beforeDigits = 1
	afterDigits = 1
	for _, h := range hunks {
		beforeLine := h.FromLine
		afterLine := h.ToLine
		for _, l := range h.Lines {
			switch l.Kind {
			case udiff.Delete:
				beforeDigits = intMax(beforeDigits, digits(beforeLine))
				beforeLine++
			case udiff.Insert:
				afterDigits = intMax(afterDigits, digits(afterLine))
				afterLine++
			default:
				beforeDigits = intMax(beforeDigits, digits(beforeLine))
				afterDigits = intMax(afterDigits, digits(afterLine))
				beforeLine++
				afterLine++
			}
		}
	}
	return
}
