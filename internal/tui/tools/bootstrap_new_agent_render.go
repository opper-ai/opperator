package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
	"tui/styles"
	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
)

func init() {
	registerBootstrapNewAgentRenderer()
}

func registerBootstrapNewAgentRenderer() {
	toolregistry.Register(BootstrapNewAgentToolName, toolregistry.Definition{
		Label: "Bootstrap",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			t := styles.CurrentTheme()
			var params BootstrapNewAgentParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			agentName := strings.TrimSpace(params.AgentName)
			title := "Bootstrap agent"
			if agentName != "" {
				title = fmt.Sprintf("Bootstrap %s", agentName)
			}
			header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + title + " ")
			return strings.TrimSpace(header + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()
			var params BootstrapNewAgentParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			agentName := strings.TrimSpace(params.AgentName)

			var meta BootstrapNewAgentMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
				if strings.TrimSpace(meta.AgentName) != "" {
					agentName = meta.AgentName
				}
			}

			if agentName == "" {
				agentName = "agent"
			}

			header := lipgloss.NewStyle().Foreground(t.FgMuted).Render(fmt.Sprintf("└ Bootstrap %s", agentName))
			content := strings.TrimSpace(result.Content)

			// Check if operation succeeded or failed
			ok := !strings.HasPrefix(strings.ToLower(content), "error:") &&
			      !strings.HasPrefix(strings.ToLower(content), "warning:")

			style := lipgloss.NewStyle().Foreground(t.Error)
			indicator := "✗ "
			if ok {
				style = lipgloss.NewStyle().Foreground(t.Success)
				indicator = "✓ "
			}

			gutter := lipgloss.NewStyle().MarginLeft(2).Foreground(t.FgMuted).Render("│ ")

			var rows []string
			rows = append(rows, header)
			rows = append(rows, "")

			// Show main message
			body := style.Render(indicator + content)
			rows = append(rows, gutter+body)

			// Show steps if available
			if len(meta.Steps) > 0 {
				rows = append(rows, gutter)
				stepStyle := lipgloss.NewStyle().Foreground(t.FgSubtle)
				for _, step := range meta.Steps {
					rows = append(rows, gutter+stepStyle.Render("  • "+step))
				}
			}

			// Show error details if present
			if meta.Error != "" {
				rows = append(rows, gutter)
				errorStyle := lipgloss.NewStyle().Foreground(t.Error)
				rows = append(rows, gutter+errorStyle.Render("  Error: "+meta.Error))
			}

			return lipgloss.JoinVertical(lipgloss.Left, rows...)
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params BootstrapNewAgentParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			if agentName := strings.TrimSpace(params.AgentName); agentName != "" {
				return "Bootstrap " + agentName
			}
			return "Bootstrap"
		},
	})
}
