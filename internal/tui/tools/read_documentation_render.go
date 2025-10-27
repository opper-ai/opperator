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
	registerReadDocumentationRenderer()
}

type ReadDocumentationMetadata struct {
	ReadDocs []string `json:"read_docs"`
}

func registerReadDocumentationRenderer() {
	toolregistry.Register("read_documentation", toolregistry.Definition{
		Label:  "Read Docs",
		Hidden: true,
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			t := styles.CurrentTheme()
			var params ReadDocumentationParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			title := "Reading documentation"
			if len(params.Names) > 0 {
				if len(params.Names) == 1 {
					title = fmt.Sprintf("Reading %s", params.Names[0])
				} else {
					title = fmt.Sprintf("Reading %d files", len(params.Names))
				}
			}

			header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + title + " ")
			return strings.TrimSpace(header + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()

			var meta ReadDocumentationMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			// Parse the result content to determine success/failure
			content := strings.TrimSpace(result.Content)
			ok := !strings.HasPrefix(strings.ToLower(content), "error:") &&
			      !strings.HasPrefix(strings.ToLower(content), "warning:")

			// Build the display message
			var displayMsg string
			if len(meta.ReadDocs) > 0 {
				if len(meta.ReadDocs) == 1 {
					displayMsg = fmt.Sprintf("Successfully read %s", meta.ReadDocs[0])
				} else {
					displayMsg = fmt.Sprintf("Successfully read %s", strings.Join(meta.ReadDocs, ", "))
				}
			} else {
				displayMsg = content
			}

			style := lipgloss.NewStyle().Foreground(t.Error)
			indicator := "✗ "
			if ok {
				style = lipgloss.NewStyle().Foreground(t.Success)
				indicator = "✓ "
			}

			header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ Read documentation")
			gutter := lipgloss.NewStyle().MarginLeft(2).Foreground(t.FgMuted).Render("│ ")

			var rows []string
			rows = append(rows, header)
			rows = append(rows, "")
			rows = append(rows, gutter+style.Render(indicator+displayMsg))

			return lipgloss.JoinVertical(lipgloss.Left, rows...)
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params ReadDocumentationParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			if len(params.Names) == 1 {
				return "Read " + params.Names[0]
			} else if len(params.Names) > 1 {
				return fmt.Sprintf("Read %d docs", len(params.Names))
			}
			return "Read documentation"
		},
	})
}
