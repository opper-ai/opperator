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
	toolregistry.Register(WriteToolName, toolregistry.Definition{
		Label: "Write",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			var params WriteParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			label := "└ Write"
			if base := filepath.Base(strings.TrimSpace(params.FilePath)); base != "" && base != "." && base != "/" {
				label = fmt.Sprintf("└ Write %s", base)
			}
			return strings.TrimSpace(label + " " + spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()

			var params WriteParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta WriteMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			filePath := strings.TrimSpace(params.FilePath)
			content := strings.TrimRight(params.Content, "\n")
			if content == "" {
				content = strings.TrimRight(result.Content, "\n")
			}
			header := "Write"
			if base := filepath.Base(filePath); base != "" && base != "." && base != "/" {
				header = fmt.Sprintf("Write %s", base)
			}
			if meta.Additions > 0 || meta.Removals > 0 {
				header = fmt.Sprintf("%s (+%d -%d)", header, meta.Additions, meta.Removals)
			}
			headerView := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + header)

			if result.IsError {
				errorMsg := lipgloss.NewStyle().Foreground(t.Error).Render(strings.TrimSpace(result.Content))
				return headerView + "\n\n" + errorMsg
			}

			preview := renderCodePreview(filePath, content, 0, width, 0)
			return headerView + "\n\n" + preview
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params WriteParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			base := filepath.Base(strings.TrimSpace(params.FilePath))
			if base == "" || base == "." || base == "/" {
				return "Write"
			}
			return "Write " + base
		},
	})
}
