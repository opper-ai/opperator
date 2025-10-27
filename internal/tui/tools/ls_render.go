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
	registerLSRenderer()
}

func registerLSRenderer() {
	toolregistry.Register(LSToolName, toolregistry.Definition{
		Label: "List",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			var params LSParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			title := "└ List"
			if base := filepath.Base(strings.TrimSpace(params.Path)); base != "" && base != "." && base != "/" {
				title = fmt.Sprintf("└ List %s", base)
			}
			return strings.TrimSpace(title + " " + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()

			var params LSParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta LSMeta
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			dir := strings.TrimSpace(meta.Path)
			if dir == "" {
				dir = strings.TrimSpace(params.Path)
			}

			header := "List"
			if dir != "" {
				base := filepath.Base(dir)
				if base == "" || base == "." || base == "/" {
					base = dir
				}
				if trimmed := strings.TrimSpace(base); trimmed != "" {
					header = fmt.Sprintf("List %s", trimmed)
				}
			}

			var details []string
			if meta.EntryCount > 0 {
				details = append(details, fmt.Sprintf("%d entries", meta.EntryCount))
			}
			if meta.Truncated {
				details = append(details, "tool limited to 200 entries")
			}
			if len(details) > 0 {
				header = fmt.Sprintf("%s (%s)", header, strings.Join(details, ", "))
			}
			headerView := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + header)

			if result.IsError {
				errorMsg := lipgloss.NewStyle().Foreground(t.Error).Render(strings.TrimSpace(result.Content))
				if strings.TrimSpace(errorMsg) == "" {
					errorMsg = lipgloss.NewStyle().Foreground(t.Error).Render("unknown error")
				}
				return headerView + "\n\n" + errorMsg
			}

			raw := strings.TrimRight(result.Content, "\n")
			lines := []string{}
			if raw != "" {
				lines = strings.Split(raw, "\n")
			}
			if len(lines) > 0 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(lines[0])), "listing for") {
				lines = lines[1:]
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
				empty := renderGutterList([]string{"Directory is empty"}, width, func(s string) string {
					return lipgloss.NewStyle().Foreground(t.FgMuted).Italic(true).Render(s)
				})
				return headerView + "\n\n" + empty
			}

			maxDisplay := len(entries)
			if maxDisplay > 12 {
				maxDisplay = 12
			}
			display := entries[:maxDisplay]
			more := len(entries) - maxDisplay

			body := renderGutterList(display, width, func(line string) string {
				if strings.HasPrefix(line, "[DIR]") {
					parts := strings.SplitN(line, " ", 2)
					label := lipgloss.NewStyle().Foreground(t.Primary).Bold(true).Render(parts[0])
					if len(parts) > 1 {
						rest := lipgloss.NewStyle().Foreground(t.FgBase).Render(" " + strings.TrimSpace(parts[1]))
						return label + rest
					}
					return label
				}
				return lipgloss.NewStyle().Foreground(t.FgBase).Render(strings.TrimSpace(line))
			})

			var footer []string
			shown := len(display)
			if meta.EntryCount > 0 {
				total := meta.EntryCount
				if shown > total {
					shown = total
				}
				if total == shown && more == 0 && !meta.Truncated {
					footer = append(footer, fmt.Sprintf("%d entries", total))
				} else {
					footer = append(footer, fmt.Sprintf("showing %d of %d", shown, total))
				}
			} else {
				footer = append(footer, fmt.Sprintf("showing %d entries", shown))
			}
			if more > 0 {
				footer = append(footer, fmt.Sprintf("… %d more not shown", more))
			}
			if meta.Truncated {
				footer = append(footer, "tool truncated additional entries")
			}

			resultView := headerView + "\n\n" + body
			if len(footer) > 0 {
				resultView += "\n\n" + lipgloss.NewStyle().MarginLeft(2).Foreground(t.FgMuted).Render(strings.Join(footer, " • "))
			}
			return resultView
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params LSParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			target := strings.TrimSpace(params.Path)
			if target == "" {
				return "List"
			}
			base := filepath.Base(target)
			if base == "" || base == "." || base == "/" {
				base = target
			}
			base = strings.TrimSpace(base)
			if base == "" {
				return "List"
			}
			return "List " + base
		},
	})
}
