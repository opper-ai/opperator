package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ManageSecretToolName   = "manage_secret"
	ipcRequestSetSecret    = "secret_set"
	ipcRequestDeleteSecret = "secret_delete"
)

// ManageSecretParams captures the parameters accepted by the manage_secret tool.
type ManageSecretParams struct {
	Mode             string `json:"mode"`
	Name             string `json:"name"`
	Title            string `json:"title"`
	Description      string `json:"description"`
	ValueLabel       string `json:"value_label"`
	DocumentationURL string `json:"documentation_url"`
	Value            string `json:"value"`
}

// ParseManageSecretParams decodes arguments into a sanitized ManageSecretParams value.
func ParseManageSecretParams(arguments string) (ManageSecretParams, error) {
	params := ManageSecretParams{}
	if strings.TrimSpace(arguments) == "" {
		return params, nil
	}
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return ManageSecretParams{}, err
	}
	return params.clean(), nil
}

func (p ManageSecretParams) clean() ManageSecretParams {
	p.Mode = strings.TrimSpace(p.Mode)
	p.Name = strings.TrimSpace(p.Name)
	p.Title = strings.TrimSpace(p.Title)
	p.Description = strings.TrimSpace(p.Description)
	p.ValueLabel = strings.TrimSpace(p.ValueLabel)
	p.DocumentationURL = strings.TrimSpace(p.DocumentationURL)
	p.Value = strings.TrimSpace(p.Value)
	return p
}

func (p ManageSecretParams) Clean() ManageSecretParams {
	return p.clean()
}

func ManageSecretSpec() Spec {
	return Spec{
		Name:        ManageSecretToolName,
		Description: "Prompt the operator for a secret value and store, update, or delete it in the Opperator keyring.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mode": map[string]any{
					"type":        "string",
					"enum":        []string{"create", "update", "upsert", "delete"},
					"description": "Action to perform for the secret. Defaults to create.",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Canonical secret name to store in the Opperator keyring.",
				},
				"title": map[string]any{
					"type":        "string",
					"description": "Optional title displayed above the secret input field.",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "Optional helper text shown to the operator before prompting for the value.",
				},
				"value_label": map[string]any{
					"type":        "string",
					"description": "Optional label or placeholder for the secret input field.",
				},
				"documentation_url": map[string]any{
					"type":        "string",
					"description": "Optional URL presented to the operator for setup instructions.",
				},
			},
			"required": []string{"name"},
		},
	}
}

// RunManageSecret executes the manage_secret tool using the daemon IPC channel.
func RunManageSecret(ctx context.Context, arguments string) (string, string) {
	params, err := ParseManageSecretParams(arguments)
	if err != nil {
		return fmt.Sprintf("error parsing parameters: %v", err), ""
	}

	mode := strings.ToLower(params.Mode)
	switch mode {
	case "", "create", "update", "upsert", "delete":
	default:
		return fmt.Sprintf("error: unsupported mode %q", params.Mode), ""
	}

	name := params.Name
	if name == "" {
		return "error: secret name is required", ""
	}

	if mode == "delete" {
		return runManageSecretDelete(ctx, name)
	}

	value := params.Value
	if value == "" {
		return "error: secret value is required", ""
	}

	payload := map[string]any{
		"type":         ipcRequestSetSecret,
		"secret_name":  name,
		"secret_value": value,
	}
	if mode != "" && mode != "create" {
		payload["mode"] = mode
	}

	respBytes, err := IPCRequestCtx(ctx, payload)
	if err != nil {
		return fmt.Sprintf("error storing secret: %v", err), ""
	}

	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return fmt.Sprintf("error decoding secret response: %v", err), ""
	}
	if !resp.Success {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return fmt.Sprintf("error storing secret: %s", errMsg), ""
	}

	action := "updated"
	if mode == "" || mode == "create" {
		action = "created"
	} else if mode == "upsert" {
		action = "stored"
	}

	metaBytes, _ := json.Marshal(map[string]any{
		"secret": map[string]any{
			"name":   name,
			"action": action,
		},
	})

	return fmt.Sprintf("secret %q %s", name, action), string(metaBytes)
}

func runManageSecretDelete(ctx context.Context, name string) (string, string) {
	respBytes, err := IPCRequestCtx(ctx, map[string]any{
		"type":        ipcRequestDeleteSecret,
		"secret_name": name,
	})
	if err != nil {
		return fmt.Sprintf("error deleting secret: %v", err), ""
	}

	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return fmt.Sprintf("error decoding secret response: %v", err), ""
	}
	if !resp.Success {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return fmt.Sprintf("error deleting secret: %s", errMsg), ""
	}

	metaBytes, _ := json.Marshal(map[string]any{
		"secret": map[string]any{
			"name":   name,
			"action": "deleted",
		},
	})
	return fmt.Sprintf("secret %q deleted", name), string(metaBytes)
}
