package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

//go:embed start_agent.md
var startAgentDescription []byte

const (
	StartAgentToolName = "start_agent"
	startAgentDelay    = 1 * time.Millisecond
)

type StartAgentParams struct {
	Name string `json:"name"`
}

type StartAgentMetadata struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	At     string `json:"at"`
}

func StartAgentSpec() Spec {
	return Spec{
		Name:        StartAgentToolName,
		Description: strings.TrimSpace(string(startAgentDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Agent name"},
			},
			"required": []string{"name"},
		},
	}
}

func RunStartAgent(ctx context.Context, arguments string) (string, string) {
	if err := sleepWithCancel(ctx, startAgentDelay); err != nil {
		return "canceled", ""
	}

	var params StartAgentParams
	_ = json.Unmarshal([]byte(arguments), &params)
	if strings.TrimSpace(params.Name) == "" {
		return "error: missing name", ""
	}

	respb, err := ipcRequestCtx(ctx, struct {
		Type      string `json:"type"`
		AgentName string `json:"agent_name"`
	}{Type: "start", AgentName: params.Name})
	if err != nil {
		return fmt.Sprintf("error: %v", err), ""
	}
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
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

	meta := StartAgentMetadata{Name: params.Name, Action: "start", At: time.Now().Format(time.RFC3339)}
	mb, _ := json.Marshal(meta)
	return fmt.Sprintf("Started agent %q", params.Name), string(mb)
}
