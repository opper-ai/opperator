package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"tui/internal/keyring"
)

const (
	ListSecretsToolName = "list_secrets"
)

func ListSecretsSpec() Spec {
	return Spec{
		Name:        ListSecretsToolName,
		Description: "List the names of secrets currently stored in the Opperator keyring.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

// RunListSecrets retrieves the registered secret names from the daemon.
func RunListSecrets(ctx context.Context, arguments string) (string, string) {
	respBytes, err := IPCRequestCtx(ctx, map[string]any{"type": ipcRequestListSecrets})
	if err != nil {
		return fmt.Sprintf("error listing secrets: %v", err), ""
	}

	var resp struct {
		Success bool     `json:"success"`
		Error   string   `json:"error"`
		Secrets []string `json:"secrets"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return fmt.Sprintf("error decoding secret list: %v", err), ""
	}
	if !resp.Success {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return fmt.Sprintf("error listing secrets: %s", errMsg), ""
	}

	names := normalizeSecretNames(resp.Secrets)
	if len(names) == 0 {
		return "No secrets have been stored yet.", marshalSecretsMetadata(nil)
	}

	lines := make([]string, 0, len(names))
	metaEntries := make([]map[string]string, 0, len(names))
	for _, name := range names {
		label := friendlySecretLabel(name)
		if label != "" {
			lines = append(lines, fmt.Sprintf("- %s — %s", name, label))
			metaEntries = append(metaEntries, map[string]string{"name": name, "label": label})
		} else {
			lines = append(lines, fmt.Sprintf("- %s", name))
			metaEntries = append(metaEntries, map[string]string{"name": name})
		}
	}

	return strings.Join(lines, "\n"), marshalSecretsMetadata(metaEntries)
}

func normalizeSecretNames(raw []string) []string {
	trimmed := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, name := range raw {
		n := strings.TrimSpace(name)
		if n == "" || seen[strings.ToLower(n)] {
			continue
		}
		seen[strings.ToLower(n)] = true
		trimmed = append(trimmed, n)
	}
	sort.Strings(trimmed)
	return trimmed
}

func friendlySecretLabel(name string) string {
	if strings.EqualFold(name, keyring.OpperAPIKeyName) {
		return "Your default Opper API key"
	}
	return ""
}

func marshalSecretsMetadata(entries []map[string]string) string {
	payload := map[string]any{"secrets": entries}
	b, _ := json.Marshal(payload)
	return string(b)
}

const ipcRequestListSecrets = "secret_list"
