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
	registerPlanRenderer()
}

func registerPlanRenderer() {
	toolregistry.Register(PlanToolName, toolregistry.Definition{
		Label: "Manage Plan",
		Hidden: true,
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			var params PlanParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			title := "└ Updating Plan"
			if params.Action == "get" {
				title = "└ Getting Plan"
			}

			return strings.TrimSpace(title + " " + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()

			var params PlanParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta PlanMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			// Build header based on action
			header := "Update Plan"
			switch params.Action {
			case "get":
				header = "Get Plan"
			}

			headerView := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + header)
			gutter := lipgloss.NewStyle().MarginLeft(2).Foreground(t.FgMuted).Render("│ ")

			if result.IsError {
				errorMsg := lipgloss.NewStyle().Foreground(t.Error).Render(strings.TrimSpace(result.Content))
				if strings.TrimSpace(errorMsg) == "" {
					errorMsg = lipgloss.NewStyle().Foreground(t.Error).Render("unknown error")
				}
				return headerView + "\n\n" + gutter + errorMsg
			}

			var b strings.Builder
			b.WriteString(headerView)
			b.WriteString("\n\n")

			// For get action, show only the specification content
			if params.Action == "get" {
				if meta.Specification != "" {
					specText := lipgloss.NewStyle().Foreground(t.FgMuted).Render(meta.Specification)
					b.WriteString(gutter + specText)
				} else {
					empty := lipgloss.NewStyle().
						Foreground(t.FgMuted).
						Italic(true).
						Render("No specification set")
					b.WriteString(gutter + empty)
				}
				return b.String()
			}

			// For set_specification action with empty spec, show "cleared" message
			if params.Action == "set_specification" && meta.Specification == "" {
				cleared := lipgloss.NewStyle().
					Foreground(t.FgMuted).
					Italic(true).
					Render("Specification cleared")
				b.WriteString(gutter + cleared)
				return b.String()
			}

			// Render the plan items (not the specification - keep it internal)
			if len(meta.Items) > 0 {
				b.WriteString(renderPlanList(meta.Items, t, width, gutter))
			} else {
				// Show truncated spec if available, otherwise "No plan items"
				if meta.Specification != "" {
					truncated := truncateSpec(meta.Specification, 100)
					specStyle := lipgloss.NewStyle().Foreground(t.FgMuted).Italic(true)
					b.WriteString(gutter + specStyle.Render("Spec: "+truncated))
				} else {
					empty := lipgloss.NewStyle().
						Foreground(t.FgMuted).
						Italic(true).
						Render("No plan items")
					b.WriteString(gutter + empty)
				}
			}

			return b.String()
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params PlanParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			if params.Action == "get" {
				return "Get Plan"
			}
			return "Update Plan"
		},
		Copy: func(call tooltypes.Call, result tooltypes.Result) string {
			var params PlanParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta PlanMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			// For get action, return the specification
			if params.Action == "get" {
				if meta.Specification != "" {
					return meta.Specification
				}
				return "No specification set"
			}

			// For other actions, return a clean list of plan items
			if len(meta.Items) == 0 {
				return "No plan items"
			}

			var b strings.Builder
			for _, item := range meta.Items {
				if item.Completed {
					b.WriteString("- [x] ")
				} else {
					b.WriteString("- [ ] ")
				}
				b.WriteString(item.Text)
				b.WriteString("\n")
			}
			return strings.TrimSpace(b.String())
		},
	})
}

func renderPlanList(items []PlanItem, t styles.Theme, width int, gutter string) string {
	var renderedItems []string

	// Count completed vs total
	completed := 0
	for _, item := range items {
		if item.Completed {
			completed++
		}
	}

	// Add progress summary
	summary := fmt.Sprintf("(%d/%d completed)", completed, len(items))
	summaryColor := t.FgMuted
	if completed == len(items) {
		summaryColor = t.Success
	}
	summaryView := lipgloss.NewStyle().Foreground(summaryColor).Render(summary)
	renderedItems = append(renderedItems, gutter+summaryView)

	// Render each plan item
	for i, item := range items {
		renderedItems = append(renderedItems, gutter+renderPlanItem(item, i+1, t))
	}

	// Join all items
	content := strings.Join(renderedItems, "\n")
	return content
}

func renderPlanItem(item PlanItem, index int, t styles.Theme) string {
	if item.Completed {
		// Completed: green checkmark + strikethrough text
		checkmark := lipgloss.NewStyle().Foreground(t.Success).Bold(true).Render("✓")
		text := lipgloss.NewStyle().
			Foreground(t.FgMuted).
			Strikethrough(true).
			Render(item.Text)
		return checkmark + " " + text
	} else {
		// Incomplete: empty checkbox + normal text
		checkbox := lipgloss.NewStyle().Foreground(t.FgSubtle).Render("☐")
		text := lipgloss.NewStyle().Foreground(t.FgBase).Render(item.Text)
		return checkbox + " " + text
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// truncateSpec truncates a specification to the given max length with ellipsis
func truncateSpec(spec string, maxLen int) string {
	// Remove newlines and extra whitespace
	cleaned := strings.Join(strings.Fields(spec), " ")

	if len(cleaned) <= maxLen {
		return cleaned
	}

	// Truncate and add ellipsis
	return cleaned[:maxLen-3] + "..."
}
