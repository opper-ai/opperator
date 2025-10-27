package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"

	"tui/styles"
	"tui/tools/registry"
	"tui/tools/types"
)

func init() {
	registry.Register(ManageSecretToolName, registry.Definition{
		Label: "Secret",
		Pending: func(call types.Call, width int, spinner string) string {
			params, err := ParseManageSecretParams(call.Input)
			if err != nil {
				return strings.TrimSpace("└ Secret " + spinner)
			}
			mode := formatSecretMode(params.Mode)
			name := strings.TrimSpace(params.Name)
			label := fmt.Sprintf("└ Secret %s %s", mode, name)
			return strings.TrimSpace(label + " " + spinner)
		},
		Render: func(call types.Call, result types.Result, width int) string {
			theme := styles.CurrentTheme()
			params, _ := ParseManageSecretParams(call.Input)
			name := strings.TrimSpace(params.Name)

			meta := struct {
				Secret struct {
					Name   string `json:"name"`
					Action string `json:"action"`
				} `json:"secret"`
			}{}
			if strings.TrimSpace(result.Metadata) != "" {
				_ = json.Unmarshal([]byte(result.Metadata), &meta)
			}

			action := strings.TrimSpace(meta.Secret.Action)
			if action == "" {
				action = formatSecretMode(params.Mode)
			}
			if strings.TrimSpace(meta.Secret.Name) != "" {
				name = strings.TrimSpace(meta.Secret.Name)
			}
			if name == "" {
				name = "secret"
			}

			content := strings.TrimSpace(result.Content)
			if strings.HasPrefix(strings.ToLower(content), "error") {
				errorStyle := lipgloss.NewStyle().Foreground(theme.Error)
				return errorStyle.Render(content)
			}
			if content == "" {
				content = fmt.Sprintf("Secret %s %s", action, name)
			}

			return fmt.Sprintf("└ %s", content)
		},
	})
}

func formatSecretMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "update":
		return "update"
	case "delete":
		return "delete"
	case "upsert":
		return "store"
	default:
		return "create"
	}
}
