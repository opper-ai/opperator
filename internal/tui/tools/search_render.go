package tools

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"

	"tui/styles"
	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
)

func init() {
	registerGlobRenderer()
	registerGrepRenderer()
	registerRGRenderer()
}

func registerGlobRenderer() {
	toolregistry.Register(GlobToolName, toolregistry.Definition{
		Label: "Glob",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			var params GlobParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			title := "└ Glob"
			pattern := strings.TrimSpace(params.Pattern)
			if pattern != "" {
				title = fmt.Sprintf("└ Glob %q", shortenText(pattern, 32))
			}
			if path := strings.TrimSpace(params.Path); path != "" {
				base := filepath.Base(path)
				if base == "" || base == "." || base == "/" {
					base = path
				}
				title += " in " + shortenText(base, 24)
			}
			return strings.TrimSpace(title + " " + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()

			var params GlobParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta GlobMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			pattern := strings.TrimSpace(meta.Pattern)
			if pattern == "" {
				pattern = strings.TrimSpace(params.Pattern)
			}
			root := strings.TrimSpace(meta.Root)
			if root == "" {
				root = strings.TrimSpace(params.Path)
			}

			header := "Glob"
			if pattern != "" {
				header = fmt.Sprintf("Glob %q", shortenText(pattern, 40))
			}
			if root != "" {
				base := filepath.Base(root)
				if base == "" || base == "." || base == "/" {
					base = root
				}
				header = fmt.Sprintf("%s in %s", header, shortenText(base, 32))
			}

			var details []string
			if meta.Matches > 0 {
				details = append(details, fmt.Sprintf("%d matches", meta.Matches))
			}
			if meta.Truncated {
				details = append(details, "truncated")
			}
			if len(details) > 0 {
				header = fmt.Sprintf("%s (%s)", header, strings.Join(details, ", "))
			}
			headerView := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + header)

			if result.IsError {
				errorMsg := lipgloss.NewStyle().Foreground(t.Error).Render(strings.TrimSpace(result.Content))
				return headerView + "\n\n" + errorMsg
			}

			if meta.Matches == 0 {
				message := strings.TrimSpace(result.Content)
				if message == "" {
					message = "No files matched the pattern"
				}
				body := renderGutterList([]string{message}, width, func(s string) string {
					return lipgloss.NewStyle().Foreground(t.FgMuted).Italic(true).Render(s)
				})
				return headerView + "\n\n" + body
			}

			raw := strings.TrimRight(result.Content, "\n")
			lines := []string{}
			if raw != "" {
				lines = strings.Split(raw, "\n")
			}

			entries := make([]string, 0, len(lines))
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if trimmed == "" {
					continue
				}
				if strings.HasPrefix(trimmed, "… results truncated …") {
					continue
				}
				entries = append(entries, trimmed)
			}
			if len(entries) == 0 {
				message := strings.TrimSpace(result.Content)
				if message == "" {
					message = "No files matched the pattern"
				}
				body := renderGutterList([]string{message}, width, func(s string) string {
					return lipgloss.NewStyle().Foreground(t.FgMuted).Italic(true).Render(s)
				})
				return headerView + "\n\n" + body
			}

			maxDisplay := len(entries)
			if maxDisplay > 12 {
				maxDisplay = 12
			}
			display := entries[:maxDisplay]
			more := len(entries) - maxDisplay

			body := renderGutterList(display, width, nil)

			var footer []string
			shown := len(display)
			if meta.Matches > 0 {
				total := meta.Matches
				if shown > total {
					shown = total
				}
				if total == shown && more == 0 && !meta.Truncated {
					footer = append(footer, fmt.Sprintf("%d matches", total))
				} else {
					footer = append(footer, fmt.Sprintf("showing %d of %d", len(display), total))
				}
			}
			if more > 0 {
				footer = append(footer, fmt.Sprintf("… %d more not shown", more))
			}
			if meta.Truncated {
				footer = append(footer, "tool truncated additional results")
			}

			resultView := headerView + "\n\n" + body
			if len(footer) > 0 {
				resultView += "\n\n" + lipgloss.NewStyle().Foreground(t.FgMuted).Render(strings.Join(footer, " • "))
			}
			return resultView
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params GlobParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			pattern := strings.TrimSpace(params.Pattern)
			if pattern == "" {
				return "Glob"
			}
			return fmt.Sprintf("Glob %q", shortenText(pattern, 24))
		},
	})
}

func registerGrepRenderer() {
	toolregistry.Register(GrepToolName, toolregistry.Definition{
		Label: "Grep",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			var params GrepParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			title := "└ Grep"
			pattern := strings.TrimSpace(params.Pattern)
			if pattern != "" {
				title = fmt.Sprintf("└ Grep %q", shortenText(pattern, 32))
			}
			if path := strings.TrimSpace(params.Path); path != "" {
				base := filepath.Base(path)
				if base == "" || base == "." || base == "/" {
					base = path
				}
				title += " in " + shortenText(base, 24)
			}
			return strings.TrimSpace(title + " " + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()

			var params GrepParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta GrepMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			pattern := strings.TrimSpace(meta.Pattern)
			if pattern == "" {
				pattern = strings.TrimSpace(params.Pattern)
			}
			root := strings.TrimSpace(meta.Root)
			if root == "" {
				root = strings.TrimSpace(params.Path)
			}

			header := "Grep"
			if pattern != "" {
				header = fmt.Sprintf("Grep %q", shortenText(pattern, 40))
			}
			if root != "" {
				base := filepath.Base(root)
				if base == "" || base == "." || base == "/" {
					base = root
				}
				header = fmt.Sprintf("%s in %s", header, shortenText(base, 32))
			}

			var details []string
			if meta.Matches > 0 {
				details = append(details, fmt.Sprintf("%d matches", meta.Matches))
			}
			if meta.Truncated {
				details = append(details, "truncated")
			}
			if len(details) > 0 {
				header = fmt.Sprintf("%s (%s)", header, strings.Join(details, ", "))
			}
			headerView := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + header)

			if result.IsError {
				errorMsg := lipgloss.NewStyle().Foreground(t.Error).Render(strings.TrimSpace(result.Content))
				return headerView + "\n\n" + errorMsg
			}

			if meta.Matches == 0 {
				message := strings.TrimSpace(result.Content)
				if message == "" {
					message = fmt.Sprintf("No matches for %q", pattern)
				}
				body := renderGutterList([]string{message}, width, func(s string) string {
					return lipgloss.NewStyle().Foreground(t.FgMuted).Italic(true).Render(s)
				})
				return headerView + "\n\n" + body
			}

			raw := strings.TrimRight(result.Content, "\n")
			lines := []string{}
			if raw != "" {
				lines = strings.Split(raw, "\n")
			}

			entries := make([]string, 0, len(lines))
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if trimmed == "" {
					continue
				}
				entries = append(entries, trimmed)
			}
			if len(entries) == 0 {
				message := fmt.Sprintf("Matches found for %q but no preview available", pattern)
				body := renderGutterList([]string{message}, width, func(s string) string {
					return lipgloss.NewStyle().Foreground(t.FgMuted).Italic(true).Render(s)
				})
				return headerView + "\n\n" + body
			}

			maxDisplay := len(entries)
			if maxDisplay > 12 {
				maxDisplay = 12
			}
			display := entries[:maxDisplay]
			more := len(entries) - maxDisplay

			transform := func(line string) string {
				trimmed := strings.TrimSpace(line)
				pathPart, linePart, snippet := splitGrepLine(trimmed)
				pathView := lipgloss.NewStyle().Foreground(t.Info).Render(pathPart)
				lineView := ""
				if linePart != "" {
					lineView = lipgloss.NewStyle().Foreground(t.FgMuted).Render(":" + linePart)
				}
				snippetView := ""
				if snippet != "" {
					snippetView = lipgloss.NewStyle().Foreground(t.FgBase).Render(": " + snippet)
				}
				return pathView + lineView + snippetView
			}
			body := renderGutterList(display, width, transform)

			var footer []string
			shown := len(display)
			if meta.Matches > 0 {
				total := meta.Matches
				if shown > total {
					shown = total
				}
				if total == shown && more == 0 && !meta.Truncated {
					footer = append(footer, fmt.Sprintf("%d matches", total))
				} else {
					footer = append(footer, fmt.Sprintf("showing %d of %d", len(display), total))
				}
			}
			if more > 0 {
				footer = append(footer, fmt.Sprintf("… %d more not shown", more))
			}
			if meta.Truncated {
				footer = append(footer, "tool truncated additional matches")
			}

			resultView := headerView + "\n\n" + body
			if len(footer) > 0 {
				resultView += "\n\n" + lipgloss.NewStyle().Foreground(t.FgMuted).Render(strings.Join(footer, " • "))
			}
			return resultView
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params GrepParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			pattern := strings.TrimSpace(params.Pattern)
			if pattern == "" {
				return "Grep"
			}
			return fmt.Sprintf("Grep %q", shortenText(pattern, 24))
		},
	})
}

func registerRGRenderer() {
	toolregistry.Register(RGToolName, toolregistry.Definition{
		Label: "RG",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			var params RGParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			title := "└ RG"
			pattern := strings.TrimSpace(params.Pattern)
			if pattern != "" {
				title = fmt.Sprintf("└ RG %q", shortenText(pattern, 32))
			}
			if path := strings.TrimSpace(params.Path); path != "" {
				base := filepath.Base(path)
				if base == "" || base == "." || base == "/" {
					base = path
				}
				title += " in " + shortenText(base, 24)
			}
			return strings.TrimSpace(title + " " + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()

			var params RGParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta RGMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			pattern := strings.TrimSpace(meta.Pattern)
			if pattern == "" {
				pattern = strings.TrimSpace(params.Pattern)
			}
			root := strings.TrimSpace(meta.Root)
			if root == "" {
				root = strings.TrimSpace(params.Path)
			}

			header := "RG"
			if pattern != "" {
				header = fmt.Sprintf("RG %q", shortenText(pattern, 40))
			}
			if root != "" {
				base := filepath.Base(root)
				if base == "" || base == "." || base == "/" {
					base = root
				}
				header = fmt.Sprintf("%s in %s", header, shortenText(base, 32))
			}

			var details []string
			if meta.Matches > 0 {
				details = append(details, fmt.Sprintf("%d files", meta.Matches))
			}
			if meta.Truncated {
				details = append(details, "truncated")
			}
			if len(details) > 0 {
				header = fmt.Sprintf("%s (%s)", header, strings.Join(details, ", "))
			}
			headerView := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + header)

			if result.IsError {
				errorMsg := lipgloss.NewStyle().Foreground(t.Error).Render(strings.TrimSpace(result.Content))
				return headerView + "\n\n" + errorMsg
			}

			if meta.Matches == 0 {
				message := strings.TrimSpace(result.Content)
				if message == "" {
					message = fmt.Sprintf("No files matched pattern %q", pattern)
				}
				body := renderGutterList([]string{message}, width, func(s string) string {
					return lipgloss.NewStyle().Foreground(t.FgMuted).Italic(true).Render(s)
				})
				return headerView + "\n\n" + body
			}

			entries := meta.Files
			if len(entries) == 0 {
				raw := strings.TrimRight(result.Content, "\n")
				if raw != "" {
					entries = strings.Split(raw, "\n")
				}
			}

			filtered := make([]string, 0, len(entries))
			for _, entry := range entries {
				trimmed := strings.TrimSpace(entry)
				if trimmed == "" {
					continue
				}
				if strings.HasPrefix(trimmed, "… results truncated …") {
					continue
				}
				filtered = append(filtered, trimmed)
			}
			if len(filtered) == 0 {
				message := fmt.Sprintf("Files matched pattern %q but no preview available", pattern)
				body := renderGutterList([]string{message}, width, func(s string) string {
					return lipgloss.NewStyle().Foreground(t.FgMuted).Italic(true).Render(s)
				})
				return headerView + "\n\n" + body
			}

			maxDisplay := len(filtered)
			if maxDisplay > 12 {
				maxDisplay = 12
			}
			display := filtered[:maxDisplay]
			more := len(filtered) - maxDisplay

			body := renderGutterList(display, width, nil)

			var footer []string
			shown := len(display)
			if meta.Matches > 0 {
				total := meta.Matches
				if shown > total {
					shown = total
				}
				if total == shown && more == 0 && !meta.Truncated {
					footer = append(footer, fmt.Sprintf("%d files", total))
				} else {
					footer = append(footer, fmt.Sprintf("showing %d of %d", len(display), total))
				}
			}
			if more > 0 {
				footer = append(footer, fmt.Sprintf("… %d more not shown", more))
			}
			if meta.Truncated {
				footer = append(footer, "tool truncated additional files")
			}

			resultView := headerView + "\n\n" + body
			if len(footer) > 0 {
				resultView += "\n\n" + lipgloss.NewStyle().Foreground(t.FgMuted).Render(strings.Join(footer, " • "))
			}
			return resultView
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params RGParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			pattern := strings.TrimSpace(params.Pattern)
			if pattern == "" {
				return "RG"
			}
			return fmt.Sprintf("RG %q", shortenText(pattern, 24))
		},
	})
}

func splitGrepLine(line string) (path, location, snippet string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", "", ""
	}
	last := strings.LastIndex(trimmed, ":")
	if last == -1 {
		return trimmed, "", ""
	}
	snippet = strings.TrimSpace(trimmed[last+1:])
	rest := strings.TrimSpace(trimmed[:last])
	second := strings.LastIndex(rest, ":")
	if second == -1 {
		return rest, "", snippet
	}
	location = strings.TrimSpace(rest[second+1:])
	path = strings.TrimSpace(rest[:second])
	if path == "" {
		path = rest
	}
	return path, location, snippet
}
