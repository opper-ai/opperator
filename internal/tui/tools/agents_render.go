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

type agentProcess struct {
	Name        string
	Status      string
	PID         int
	Description string
}

func init() {
	registerListAgentsRenderer()
	registerStartAgentRenderer()
	registerStopAgentRenderer()
	registerRestartAgentRenderer()
	registerGetLogsRenderer()
	registerFocusAgentRenderer()
}

func registerListAgentsRenderer() {
	toolregistry.Register(ListAgentsToolName, toolregistry.Definition{
		Label: "Agents",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			t := styles.CurrentTheme()
			var params ListAgentsParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			title := "Agents"
			if status := strings.TrimSpace(params.Status); status != "" && status != "all" {
				title += " (" + status + ")"
			}
			header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + title + " ")
			return strings.TrimSpace(header + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()
			var params ListAgentsParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta struct {
				Processes    []agentProcess    `json:"processes"`
				Status       string            `json:"status"`
				Count        int               `json:"count"`
				Agents       []string          `json:"agents"`
				Descriptions map[string]string `json:"descriptions"`
			}
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			title := "Agents"
			statusLabel := strings.TrimSpace(meta.Status)
			if statusLabel == "" {
				statusLabel = strings.TrimSpace(params.Status)
			}
			if statusLabel != "" && statusLabel != "all" {
				title = fmt.Sprintf("Agents (%s)", statusLabel)
			}

			entries := meta.Processes
			if len(entries) == 0 && len(meta.Agents) > 0 {
				fallback := make([]agentProcess, 0, len(meta.Agents))
				for _, raw := range meta.Agents {
					name := strings.TrimSpace(raw)
					if name == "" {
						continue
					}
					desc := ""
					if meta.Descriptions != nil {
						if v, ok := meta.Descriptions[name]; ok {
							desc = strings.TrimSpace(v)
						}
					}
					fallback = append(fallback, agentProcess{Name: name, Description: desc})
				}
				entries = fallback
			}

			n := len(entries)
			count := meta.Count
			if count == 0 {
				count = n
			}
			if count > 0 {
				title = fmt.Sprintf("%s (%d)", title, count)
			}

			header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + strings.TrimSpace(title))
			gutter := lipgloss.NewStyle().MarginLeft(2).Foreground(t.FgMuted).Render("│ ")
			gutterWidth := lipgloss.Width(gutter)

			contentWidth := width
			if contentWidth <= 0 {
				contentWidth = 80
			}
			maxLineWidth := contentWidth - gutterWidth

			rows := []string{header, ""}

			if n == 0 {
				trimmed := strings.TrimSpace(result.Content)
				if trimmed == "" && len(meta.Agents) > 0 {
					trimmed = strings.Join(meta.Agents, "\n")
				}
				if trimmed == "" {
					body := lipgloss.NewStyle().Foreground(t.FgSubtle).Render("No agents found")
					rows = append(rows, gutter+body)
					return lipgloss.JoinVertical(lipgloss.Left, rows...)
				}
				contentLines := strings.Split(trimmed, "\n")
				for _, line := range contentLines {
					rows = append(rows, gutter+line)
				}
				return lipgloss.JoinVertical(lipgloss.Left, rows...)
			}

			maxDisplay := n
			if maxDisplay > 10 {
				maxDisplay = 10
			}

			descStyle := lipgloss.NewStyle().Foreground(t.FgSubtle)

			for i := 0; i < maxDisplay; i++ {
				entry := entries[i]
				status := strings.ToLower(strings.TrimSpace(entry.Status))
				statusView := lipgloss.NewStyle().Foreground(t.Warning).Render(status)
				switch status {
				case "running":
					statusView = lipgloss.NewStyle().Foreground(t.Success).Render(status)
				case "crashed":
					statusView = lipgloss.NewStyle().Foreground(t.Error).Render(status)
				case "stopped":
					statusView = lipgloss.NewStyle().Foreground(t.FgMuted).Render(status)
				}
				pid := ""
				if entry.PID > 0 {
					pid = lipgloss.NewStyle().Foreground(t.FgMuted).Render(fmt.Sprintf(" pid %d", entry.PID))
				}
				name := lipgloss.NewStyle().Foreground(t.FgBase).Render(strings.TrimSpace(entry.Name))
				desc := strings.TrimSpace(entry.Description)
				if desc == "" && meta.Descriptions != nil {
					if v, ok := meta.Descriptions[name]; ok {
						desc = strings.TrimSpace(v)
					}
				}
				rows = append(rows, gutter+fmt.Sprintf("%s — %s%s", name, statusView, pid))
				if desc != "" {
					desc = strings.ReplaceAll(desc, "\n", " ")
					desc = strings.ReplaceAll(desc, "\r", " ")
					desc = strings.ReplaceAll(desc, "\t", " ")
					for strings.Contains(desc, "  ") {
						desc = strings.ReplaceAll(desc, "  ", " ")
					}
					desc = strings.TrimSpace(desc)

					styledDesc := descStyle.Render(desc)
					truncated := truncateWidth(styledDesc, maxLineWidth)
					rows = append(rows, gutter+truncated)
				}
				if i < maxDisplay-1 {

					rows = append(rows, gutter+"")
				}
			}

			if n > maxDisplay {
				more := lipgloss.NewStyle().Foreground(t.FgSubtle).Render(fmt.Sprintf("… and %d more", n-maxDisplay))
				rows = append(rows, gutter+more)
			}
			return lipgloss.JoinVertical(lipgloss.Left, rows...)
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params ListAgentsParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			title := "Agents"
			if status := strings.TrimSpace(params.Status); status != "" && status != "all" {
				title += " (" + status + ")"
			}
			return title
		},
	})
}

func registerStartAgentRenderer() {
	toolregistry.Register(StartAgentToolName, toolregistry.Definition{
		Label: "Start",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			t := styles.CurrentTheme()
			var params StartAgentParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			target := strings.TrimSpace(params.Name)
			title := "Start agent"
			if target != "" {
				title = fmt.Sprintf("Start %s", filepath.Base(target))
			}
			header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + title + " ")
			return strings.TrimSpace(header + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			return renderAgentAction("Start", call, result)
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params StartAgentParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			if name := strings.TrimSpace(params.Name); name != "" {
				return "Start " + filepath.Base(name)
			}
			return "Start"
		},
	})
}

func registerStopAgentRenderer() {
	toolregistry.Register(StopAgentToolName, toolregistry.Definition{
		Label: "Stop",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			t := styles.CurrentTheme()
			var params StopAgentParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			target := strings.TrimSpace(params.Name)
			title := "Stop agent"
			if target != "" {
				title = fmt.Sprintf("Stop %s", filepath.Base(target))
			}
			header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + title + " ")
			return strings.TrimSpace(header + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			return renderAgentAction("Stop", call, result)
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params StopAgentParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			if name := strings.TrimSpace(params.Name); name != "" {
				return "Stop " + filepath.Base(name)
			}
			return "Stop"
		},
	})
}

func registerRestartAgentRenderer() {
	toolregistry.Register(RestartAgentToolName, toolregistry.Definition{
		Label: "Restart",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			t := styles.CurrentTheme()
			var params RestartAgentParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			target := strings.TrimSpace(params.Name)
			title := "Restart agent"
			if target != "" {
				title = fmt.Sprintf("Restart %s", filepath.Base(target))
			}
			header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + title + " ")
			return strings.TrimSpace(header + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			return renderAgentAction("Restart", call, result)
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params RestartAgentParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			if name := strings.TrimSpace(params.Name); name != "" {
				return "Restart " + filepath.Base(name)
			}
			return "Restart"
		},
	})
}

func registerGetLogsRenderer() {
	toolregistry.Register(GetLogsToolName, toolregistry.Definition{
		Label: "Logs",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			t := styles.CurrentTheme()
			var params GetLogsParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			title := "Logs"
			if name := strings.TrimSpace(params.Name); name != "" {
				title = fmt.Sprintf("Logs %s", filepath.Base(name))
			}
			if params.Lines > 0 {
				title += fmt.Sprintf(" (last %d)", params.Lines)
			}
			header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + title + " ")
			return strings.TrimSpace(header + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()
			var params GetLogsParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta GetLogsMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			name := strings.TrimSpace(meta.Name)
			if name == "" {
				name = params.Name
			}
			base := filepath.Base(strings.TrimSpace(name))
			title := "Logs " + base
			if params.Lines > 0 {
				title += fmt.Sprintf(" (last %d)", params.Lines)
			}
			header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + title)

			raw := strings.TrimSuffix(result.Content, "\n")
			lines := []string{}
			if strings.TrimSpace(raw) != "" {
				lines = strings.Split(raw, "\n")
			}

			truncatedView := false
			if len(lines) > 5 {
				lines = lines[len(lines)-5:]
				truncatedView = true
			}

			gutter := lipgloss.NewStyle().MarginLeft(2).Foreground(t.FgMuted).Render("│ ")
			gutterW := lipgloss.Width(gutter)
			contentWidth := width
			if contentWidth <= 0 {
				contentWidth = 80
			}
			minWidth := gutterW + 8
			if contentWidth < minWidth {
				contentWidth = minWidth
			}
			maxLineWidth := contentWidth - gutterW

			colorize := func(s string) string {
				lower := strings.ToLower(s)
				style := t.S().Base.Foreground(t.FgBase)
				switch {
				case strings.Contains(lower, "fatal"):
					style = style.Foreground(t.Error).Bold(true)
				case strings.Contains(lower, "error") || strings.Contains(lower, " err ") || strings.HasPrefix(lower, "err "):
					style = style.Foreground(t.Error)
				case strings.Contains(lower, "warn"):
					style = style.Foreground(t.Warning)
				case strings.Contains(lower, "info"):
					style = style.Foreground(t.Info)
				case strings.Contains(lower, "debug"):
					style = style.Foreground(t.FgMuted)
				}
				return style.Render(s)
			}

			var b strings.Builder
			b.WriteString(header)

			if len(lines) == 0 {
				b.WriteString("\n\n")
				b.WriteString(gutter)
				b.WriteString(lipgloss.NewStyle().Foreground(t.FgMuted).Italic(true).Render("No logs to display"))
				if meta.Total > 0 || meta.Returned > 0 {
					b.WriteString("\n")
					info := fmt.Sprintf("showing %d of %d", meta.Returned, meta.Total)
					if params.Lines > 0 {
						info += fmt.Sprintf(", requested last %d", params.Lines)
					}
					b.WriteString(lipgloss.NewStyle().Foreground(t.FgMuted).Render("\n" + info))
				}
				return b.String()
			}

			b.WriteString("\n\n")
			for i, line := range lines {
				if i > 0 {
					b.WriteString("\n")
				}
				b.WriteString(gutter)
				b.WriteString(colorize(truncateWidth(line, maxLineWidth)))
			}

			var footer []string
			if meta.Total > 0 || meta.Returned > 0 {
				footer = append(footer, fmt.Sprintf("showing %d of %d", meta.Returned, meta.Total))
			}
			if params.Lines > 0 {
				footer = append(footer, fmt.Sprintf("requested last %d", params.Lines))
			}
			if truncatedView {
				footer = append(footer, "view truncated to last 5 lines")
			}
			if len(footer) > 0 {
				b.WriteString("\n\n")
				b.WriteString(lipgloss.NewStyle().Foreground(t.FgMuted).Render(strings.Join(footer, " • ")))
			}

			return b.String()
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params GetLogsParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			title := "Logs"
			if name := strings.TrimSpace(params.Name); name != "" {
				title = "Logs " + filepath.Base(name)
			}
			if params.Lines > 0 {
				title += fmt.Sprintf(" (last %d)", params.Lines)
			}
			return title
		},
	})
}

func renderAgentAction(label string, call tooltypes.Call, result tooltypes.Result) string {
	t := styles.CurrentTheme()
	name := ""

	switch label {
	case "Start":
		var params StartAgentParams
		_ = json.Unmarshal([]byte(call.Input), &params)
		name = params.Name
		var meta StartAgentMetadata
		if result.Metadata != "" {
			_ = json.Unmarshal([]byte(result.Metadata), &meta)
			if strings.TrimSpace(meta.Name) != "" {
				name = meta.Name
			}
		}
	case "Stop":
		var params StopAgentParams
		_ = json.Unmarshal([]byte(call.Input), &params)
		name = params.Name
		var meta StopAgentMetadata
		if result.Metadata != "" {
			_ = json.Unmarshal([]byte(result.Metadata), &meta)
			if strings.TrimSpace(meta.Name) != "" {
				name = meta.Name
			}
		}
	case "Restart":
		var params RestartAgentParams
		_ = json.Unmarshal([]byte(call.Input), &params)
		name = params.Name
		var meta RestartAgentMetadata
		if result.Metadata != "" {
			_ = json.Unmarshal([]byte(result.Metadata), &meta)
			if strings.TrimSpace(meta.Name) != "" {
				name = meta.Name
			}
		}
	}

	trimmed := filepath.Base(strings.TrimSpace(name))
	if trimmed == "" {
		trimmed = strings.TrimSpace(name)
	}
	if trimmed == "" {
		trimmed = "agent"
	}

	header := lipgloss.NewStyle().Foreground(t.FgMuted).Render(fmt.Sprintf("└ %s %s", label, trimmed))
	content := strings.TrimSpace(result.Content)
	ok := !strings.HasPrefix(strings.ToLower(content), "error:")
	style := lipgloss.NewStyle().Foreground(t.Error)
	indicator := "✗ "
	if ok {
		style = lipgloss.NewStyle().Foreground(t.Success)
		indicator = "✓ "
	}
	gutter := lipgloss.NewStyle().MarginLeft(2).Foreground(t.FgMuted).Render("│ ")
	body := style.Render(indicator + content)
	return header + "\n\n" + gutter + body
}

func registerFocusAgentRenderer() {
	toolregistry.Register(FocusAgentToolName, toolregistry.Definition{
		Label: "Focus",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			t := styles.CurrentTheme()
			var params FocusAgentParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			target := strings.TrimSpace(params.AgentName)
			title := "Focus agent"
			if target != "" {
				title = fmt.Sprintf("Focus on %s", filepath.Base(target))
			} else {
				title = "Clear focus"
			}
			header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + title + " ")
			return strings.TrimSpace(header + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()
			var params FocusAgentParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta FocusAgentMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			agentName := strings.TrimSpace(meta.AgentName)
			if agentName == "" {
				agentName = strings.TrimSpace(params.AgentName)
			}

			action := meta.Action
			if action == "" {
				if agentName == "" {
					action = "clear"
				} else {
					action = "focus"
				}
			}

			var title string
			if action == "clear" {
				title = "Clear focus"
			} else {
				baseName := filepath.Base(agentName)
				if baseName == "" {
					baseName = agentName
				}
				title = fmt.Sprintf("Focus on %s", baseName)
			}

			header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + title)
			content := strings.TrimSpace(result.Content)
			ok := !strings.HasPrefix(strings.ToLower(content), "error:")

			style := lipgloss.NewStyle().Foreground(t.Error)
			indicator := "✗ "
			if ok {
				style = lipgloss.NewStyle().Foreground(t.Success)
				indicator = "✓ "
			}

			gutter := lipgloss.NewStyle().MarginLeft(2).Foreground(t.FgMuted).Render("│ ")
			body := style.Render(indicator + content)
			return header + "\n\n" + gutter + body
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params FocusAgentParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			if name := strings.TrimSpace(params.AgentName); name != "" {
				return "Focus on " + filepath.Base(name)
			}
			return "Clear focus"
		},
	})
}
