package tools

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss/v2"

	"tui/styles"
	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
)

func init() {
	registerDiagnosticsRenderer()
}

func registerDiagnosticsRenderer() {
	toolregistry.Register(DiagnosticsToolName, toolregistry.Definition{
		Label: "Diagnostics",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			var params DiagnosticsParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			title := "└ Diagnostics"
			if path := strings.TrimSpace(params.Path); path != "" {
				base := filepath.Base(path)
				if base == "" || base == "." || base == "/" {
					base = path
				}
				title = fmt.Sprintf("└ Diagnostics %s", shortenText(base, 32))
			}
			return strings.TrimSpace(title + " " + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()

			var params DiagnosticsParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta DiagnosticsMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			target := strings.TrimSpace(meta.Path)
			if target == "" {
				target = strings.TrimSpace(meta.AbsolutePath)
			}
			if target == "" {
				target = strings.TrimSpace(params.Path)
			}

			header := "Diagnostics"
			if target != "" {
				base := filepath.Base(target)
				if base == "" || base == "." || base == "/" {
					base = target
				}
				header = fmt.Sprintf("Diagnostics %s", shortenText(base, 32))
			}

			var details []string
			if meta.ClientCount > 0 {
				details = append(details, fmt.Sprintf("%d clients", meta.ClientCount))
			}
			if meta.FileDiagnostics > 0 {
				details = append(details, fmt.Sprintf("%d file", meta.FileDiagnostics))
			}
			if meta.ProjectDiagnostics > 0 {
				details = append(details, fmt.Sprintf("%d project", meta.ProjectDiagnostics))
			}
			if status := strings.TrimSpace(meta.Status); status != "" && status != "ok" {
				details = append(details, status)
			}
			if len(details) > 0 {
				header = fmt.Sprintf("%s (%s)", header, strings.Join(details, ", "))
			}
			headerView := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + header)

			if result.IsError {
				errorMsg := lipgloss.NewStyle().Foreground(t.Error).Render(strings.TrimSpace(result.Content))
				return headerView + "\n\n" + errorMsg
			}

			fileLines, projectLines, summaryLines, generalLines := parseDiagnosticsSections(result.Content)
			hasDiagnostics := len(fileLines) > 0 || len(projectLines) > 0

			status := strings.TrimSpace(meta.Status)
			message := strings.TrimSpace(result.Content)

			if !hasDiagnostics {
				if message == "" {
					switch status {
					case "no_clients":
						message = "No language servers are configured for diagnostics."
					case "unavailable":
						message = "Diagnostics are not available."
					case "error":
						message = "Diagnostics error"
					default:
						message = "No diagnostics reported."
					}
				}
				style := lipgloss.NewStyle().Foreground(t.FgMuted).Italic(true)
				lower := strings.ToLower(message)
				if strings.HasPrefix(lower, "error:") || status == "error" {
					style = lipgloss.NewStyle().Foreground(t.Error)
				} else if status == "no_clients" {
					style = lipgloss.NewStyle().Foreground(t.Warning)
				}
				sections := []string{renderGutterList([]string{message}, width, func(s string) string {
					return style.Render(s)
				})}
				if len(meta.Warnings) > 0 {
					warnings := renderGutterList(meta.Warnings, width, func(s string) string {
						return lipgloss.NewStyle().Foreground(t.Warning).Render(s)
					})
					sections = append(sections, gutterLabel("Warnings")+"\n"+warnings)
				}
				return headerView + "\n\n" + strings.Join(sections, "\n\n")
			}

			sections := make([]string, 0, 4)
			if len(meta.Warnings) > 0 {
				warnings := renderGutterList(meta.Warnings, width, func(s string) string {
					return lipgloss.NewStyle().Foreground(t.Warning).Render(s)
				})
				sections = append(sections, gutterLabel("Warnings")+"\n"+warnings)
			}
			if len(generalLines) > 0 {
				general := renderGutterList(generalLines, width, func(s string) string {
					return lipgloss.NewStyle().Foreground(t.FgBase).Render(s)
				})
				sections = append(sections, general)
			}
			if len(fileLines) > 0 {
				transform := func(line string) string {
					lower := strings.ToLower(line)
					switch {
					case strings.HasPrefix(lower, "error"):
						return lipgloss.NewStyle().Foreground(t.Error).Render(line)
					case strings.HasPrefix(lower, "warn"):
						return lipgloss.NewStyle().Foreground(t.Warning).Render(line)
					default:
						return lipgloss.NewStyle().Foreground(t.FgBase).Render(line)
					}
				}
				fileBody := renderGutterList(fileLines, width, transform)
				sections = append(sections, gutterLabel("File diagnostics")+"\n"+fileBody)
			}
			if len(projectLines) > 0 {
				transform := func(line string) string {
					lower := strings.ToLower(line)
					switch {
					case strings.HasPrefix(lower, "error"):
						return lipgloss.NewStyle().Foreground(t.Error).Render(line)
					case strings.HasPrefix(lower, "warn"):
						return lipgloss.NewStyle().Foreground(t.Warning).Render(line)
					default:
						return lipgloss.NewStyle().Foreground(t.FgBase).Render(line)
					}
				}
				projectBody := renderGutterList(projectLines, width, transform)
				sections = append(sections, gutterLabel("Project diagnostics")+"\n"+projectBody)
			}
			if len(summaryLines) > 0 {
				summaryBody := renderGutterList(summaryLines, width, func(s string) string {
					return lipgloss.NewStyle().Foreground(t.FgMuted).Render(s)
				})
				sections = append(sections, gutterLabel("Summary")+"\n"+summaryBody)
			}

			body := strings.Join(sections, "\n\n")

			var footer []string
			if abs := strings.TrimSpace(meta.AbsolutePath); abs != "" {
				footer = append(footer, "path "+shortenText(abs, 48))
			}
			if generated := strings.TrimSpace(meta.Generated); generated != "" {
				if ts, err := time.Parse(time.RFC3339, generated); err == nil {
					footer = append(footer, "generated "+ts.Local().Format("2006-01-02 15:04:05"))
				} else {
					footer = append(footer, "generated "+generated)
				}
			}

			resultView := headerView + "\n\n" + body
			if len(footer) > 0 {
				resultView += "\n\n" + lipgloss.NewStyle().Foreground(t.FgMuted).Render(strings.Join(footer, " • "))
			}
			return resultView
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params DiagnosticsParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			target := strings.TrimSpace(params.Path)
			if target == "" {
				return "Diagnostics"
			}
			base := filepath.Base(target)
			if base == "" || base == "." || base == "/" {
				base = target
			}
			base = strings.TrimSpace(base)
			if base == "" {
				return "Diagnostics"
			}
			return "Diagnostics " + base
		},
	})
}

func parseDiagnosticsSections(content string) (fileLines, projectLines, summaryLines, generalLines []string) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, nil, nil, nil
	}
	lines := strings.Split(content, "\n")
	section := ""
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		switch trimmedLine {
		case "<file_diagnostics>":
			section = "file"
			continue
		case "</file_diagnostics>":
			section = ""
			continue
		case "<project_diagnostics>":
			section = "project"
			continue
		case "</project_diagnostics>":
			section = ""
			continue
		case "<diagnostic_summary>":
			section = "summary"
			continue
		case "</diagnostic_summary>":
			section = ""
			continue
		}
		if trimmedLine == "" {
			continue
		}
		switch section {
		case "file":
			fileLines = append(fileLines, trimmedLine)
		case "project":
			projectLines = append(projectLines, trimmedLine)
		case "summary":
			summaryLines = append(summaryLines, trimmedLine)
		default:
			generalLines = append(generalLines, trimmedLine)
		}
	}
	return fileLines, projectLines, summaryLines, generalLines
}
