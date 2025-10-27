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
	toolregistry.Register(ViewToolName, toolregistry.Definition{
		Label: "View",
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			var params ViewParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			if base := filepath.Base(strings.TrimSpace(params.FilePath)); base != "" && base != "." && base != "/" {
				return strings.TrimSpace(fmt.Sprintf("└ View %s %s", base, spinner))
			}
			return strings.TrimSpace(fmt.Sprintf("└ View %s", spinner))
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			t := styles.CurrentTheme()
			var params ViewParams
			_ = json.Unmarshal([]byte(call.Input), &params)

			var meta ViewMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			filePath := strings.TrimSpace(meta.FilePath)
			if filePath == "" {
				filePath = strings.TrimSpace(params.FilePath)
			}

			content := strings.TrimRight(result.Content, "\n")

			header := "View"
			if base := filepath.Base(filePath); base != "" && base != "." && base != "/" {
				header = fmt.Sprintf("View %s", base)
			}
			var details []string
			if params.Offset > 0 {
				details = append(details, fmt.Sprintf("offset %d", params.Offset))
			}
			if params.Limit > 0 {
				details = append(details, fmt.Sprintf("limit %d", params.Limit))
			}
			if len(details) > 0 {
				header = fmt.Sprintf("%s (%s)", header, strings.Join(details, ", "))
			}
			headerView := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + header)

			if result.IsError {
				errorMsg := lipgloss.NewStyle().Foreground(t.Error).Render(strings.TrimSpace(result.Content))
				return headerView + "\n\n" + errorMsg
			}

			preview := renderCodePreview(filePath, content, params.Offset, width, 0)
			return headerView + "\n\n" + preview
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			var params ViewParams
			_ = json.Unmarshal([]byte(call.Input), &params)
			var meta ViewMetadata
			if result.Metadata != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}
			fp := strings.TrimSpace(meta.FilePath)
			if fp == "" {
				fp = strings.TrimSpace(params.FilePath)
			}
			base := filepath.Base(fp)
			if base == "" || base == "." || base == "/" {
				base = fp
			}
			if strings.TrimSpace(base) == "" {
				return "View"
			}
			return "View " + base
		},
	})
}
