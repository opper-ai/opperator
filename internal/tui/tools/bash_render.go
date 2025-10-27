package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss/v2"

	"tui/styles"
	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
)

func init() {
	registerBashRenderer()
}

func registerBashRenderer() {
	toolregistry.Register(BashToolName, toolregistry.Definition{
		Label: "Bash",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			var params BashParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			title := "└ Bash"
			command := strings.TrimSpace(params.Command)
			if command != "" {
				title = fmt.Sprintf("└ Bash %s", shortenText(command, 32))
			}
			return strings.TrimSpace(title + " " + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()

			var params BashParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta BashMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			command := strings.TrimSpace(meta.Command)
			if command == "" {
				command = strings.TrimSpace(params.Command)
			}

			header := "Bash"
			if command != "" {
				header = fmt.Sprintf("Bash %s", shortenText(command, 40))
			}
			headerView := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + header)

			if result.IsError {
				errorMsg := lipgloss.NewStyle().Foreground(t.Error).Render(strings.TrimSpace(result.Content))
				return headerView + "\n\n" + errorMsg
			}

			statusText := "✓ Completed"
			statusStyle := lipgloss.NewStyle().Foreground(t.Success)
			if meta.TimedOut {
				statusText = "✗ Timed out"
				exit := strings.TrimSpace(meta.ExitError)
				lower := strings.ToLower(exit)
				if exit != "" && lower != "timeout" {
					statusText += ": " + exit
				}
				statusStyle = lipgloss.NewStyle().Foreground(t.Error)
			} else if exit := strings.TrimSpace(meta.ExitError); exit != "" {
				statusText = "✗ " + exit
				statusStyle = lipgloss.NewStyle().Foreground(t.Error)
			} else if strings.HasPrefix(strings.ToLower(strings.TrimSpace(result.Content)), "error:") {
				statusText = "✗ Error"
				statusStyle = lipgloss.NewStyle().Foreground(t.Error)
			}

			statusBlock := renderGutterList([]string{statusText}, width, func(s string) string {
				return statusStyle.Render(s)
			})

			rawOutput := strings.TrimRight(result.Content, "\n")
			lines := []string{}
			if rawOutput != "" {
				lines = strings.Split(rawOutput, "\n")
			}
			if len(lines) == 0 {
				lines = []string{BashNoOutput}
			}

			maxDisplay := len(lines)
			if maxDisplay > 12 {
				maxDisplay = 12
			}
			display := lines[:maxDisplay]
			more := len(lines) - maxDisplay

			outputTransform := func(line string) string {
				trimmed := strings.TrimRight(line, "\r")
				if strings.TrimSpace(trimmed) == BashNoOutput {
					return lipgloss.NewStyle().Foreground(t.FgMuted).Italic(true).Render("No output")
				}
				return lipgloss.NewStyle().Foreground(t.FgBase).Render(trimmed)
			}
			outputBody := renderGutterList(display, width, outputTransform)

			sections := []string{statusBlock}
			if strings.TrimSpace(outputBody) != "" {
				sections = append(sections, gutterLabel("Output")+"\n"+outputBody)
			}

			var footer []string
			if more > 0 {
				footer = append(footer, fmt.Sprintf("… %d more lines", more))
			}
			if meta.OutputTruncated {
				footer = append(footer, "tool truncated output")
			}
			if dir := strings.TrimSpace(meta.WorkingDirectory); dir != "" {
				footer = append(footer, "dir "+dir)
			}
			if dur := formatBashDuration(meta.StartTime, meta.EndTime); dur != "" {
				footer = append(footer, "duration "+dur)
			}

			resultView := headerView
			if len(sections) > 0 {
				resultView += "\n\n" + strings.Join(sections, "\n")
			}
			if len(footer) > 0 {
				resultView += "\n\n" + lipgloss.NewStyle().MarginLeft(2).Foreground(t.FgMuted).Render(strings.Join(footer, " • "))
			}
			return resultView
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params BashParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			command := strings.TrimSpace(params.Command)
			if command == "" {
				return "Bash"
			}
			return "Bash " + shortenText(command, 24)
		},
	})
}

func formatBashDuration(start, end string) string {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if start == "" || end == "" {
		return ""
	}
	st, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return ""
	}
	et, err := time.Parse(time.RFC3339, end)
	if err != nil || et.Before(st) {
		return ""
	}
	duration := et.Sub(st)
	if duration <= 0 {
		return ""
	}
	switch {
	case duration < time.Millisecond:
		return duration.String()
	case duration < time.Second:
		return duration.Round(time.Millisecond).String()
	case duration < time.Minute:
		return duration.Round(10 * time.Millisecond).String()
	default:
		return duration.Round(time.Second).String()
	}
}
