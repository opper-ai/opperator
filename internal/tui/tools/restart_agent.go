package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

//go:embed restart_agent.md
var restartAgentDescription []byte

const (
	RestartAgentToolName = "restart_agent"
	restartAgentDelay    = 1 * time.Millisecond
)

type RestartAgentParams struct {
	Name string `json:"name"`
}

type RestartAgentMetadata struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	At     string `json:"at"`
}

func RestartAgentSpec() Spec {
	return Spec{
		Name:        RestartAgentToolName,
		Description: strings.TrimSpace(string(restartAgentDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Agent name"},
			},
			"required": []string{"name"},
		},
	}
}

func RunRestartAgent(ctx context.Context, arguments string) (string, string) {
	if err := sleepWithCancel(ctx, restartAgentDelay); err != nil {
		return "canceled", ""
	}

	var params RestartAgentParams
	_ = json.Unmarshal([]byte(arguments), &params)
	if strings.TrimSpace(params.Name) == "" {
		return "error: missing name", ""
	}

	// Find which daemon has this agent
	daemonName, err := FindAgentDaemon(ctx, params.Name)
	if err != nil {
		return fmt.Sprintf("error: %v", err), ""
	}

	// Send restart request to the appropriate daemon
	respb, err := ipcRequestToDaemon(ctx, daemonName, struct {
		Type      string `json:"type"`
		AgentName string `json:"agent_name"`
	}{Type: "restart", AgentName: params.Name})
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

	meta := RestartAgentMetadata{Name: params.Name, Action: "restart", At: time.Now().Format(time.RFC3339)}
	mb, _ := json.Marshal(meta)

	// Include daemon info in response
	daemonSuffix := ""
	if daemonName != "local" {
		daemonSuffix = fmt.Sprintf(" on daemon %q", daemonName)
	}
	return fmt.Sprintf("Restarted agent %q%s", params.Name, daemonSuffix), string(mb)
}
