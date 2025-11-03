package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

//go:embed get_logs.md
var getLogsDescription []byte

const (
	GetLogsToolName = "get_logs"
	getLogsDelay    = 1 * time.Millisecond
)

type GetLogsParams struct {
	Name  string `json:"name"`
	Lines int    `json:"lines"`
}

type GetLogsMetadata struct {
	Name      string `json:"name"`
	Lines     int    `json:"lines"`
	Returned  int    `json:"returned"`
	Total     int    `json:"total"`
	Retrieved string `json:"retrieved"`
}

func GetLogsSpec() Spec {
	return Spec{
		Name:        GetLogsToolName,
		Description: strings.TrimSpace(string(getLogsDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Agent name"},
				"lines": map[string]any{
					"type":        "integer",
					"description": "Optional number of lines (default 20)",
					"default":     20,
				},
			},
			"required": []string{"name"},
		},
	}
}

func RunGetLogs(ctx context.Context, arguments string) (string, string) {
	if err := sleepWithCancel(ctx, getLogsDelay); err != nil {
		return "canceled", ""
	}

	var params GetLogsParams
	_ = json.Unmarshal([]byte(arguments), &params)
	if strings.TrimSpace(params.Name) == "" {
		return "error: missing name", ""
	}

	if params.Lines <= 0 {
		params.Lines = 20
	}

	// Find which daemon the agent belongs to
	daemonName, err := FindAgentDaemon(ctx, params.Name)
	if err != nil {
		return fmt.Sprintf("error: %v", err), ""
	}

	// Query logs from the correct daemon
	respb, err := ipcRequestToDaemon(ctx, daemonName, struct {
		Type      string `json:"type"`
		AgentName string `json:"agent_name"`
	}{Type: "get_logs", AgentName: params.Name})
	if err != nil {
		return fmt.Sprintf("error: %v", err), ""
	}
	var resp struct {
		Success bool     `json:"success"`
		Error   string   `json:"error"`
		Logs    []string `json:"logs"`
	}
	if err := json.Unmarshal(respb, &resp); err != nil {
		return fmt.Sprintf("error decoding response: %v", err), ""
	}
	if !resp.Success {
		if strings.TrimSpace(resp.Error) == "" {
			resp.Error = "unknown error"
		}
		return "error: " + resp.Error, ""
	}

	lines := resp.Logs
	if params.Lines > 0 && len(lines) > params.Lines {
		lines = lines[len(lines)-params.Lines:]
	}

	content := strings.Join(lines, "\n")
	meta := GetLogsMetadata{
		Name:      params.Name,
		Lines:     params.Lines,
		Returned:  len(lines),
		Total:     len(resp.Logs),
		Retrieved: time.Now().Format(time.RFC3339),
	}
	mb, _ := json.Marshal(meta)
	return content, string(mb)
}
