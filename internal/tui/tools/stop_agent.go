package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

//go:embed stop_agent.md
var stopAgentDescription []byte

const (
	StopAgentToolName = "stop_agent"
	stopAgentDelay    = 1 * time.Millisecond
)

type StopAgentParams struct {
	Name string `json:"name"`
}

type StopAgentMetadata struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	At     string `json:"at"`
}

func StopAgentSpec() Spec {
	return Spec{
		Name:        StopAgentToolName,
		Description: strings.TrimSpace(string(stopAgentDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Agent name"},
			},
			"required": []string{"name"},
		},
	}
}

func RunStopAgent(ctx context.Context, arguments string) (string, string) {
	if err := sleepWithCancel(ctx, stopAgentDelay); err != nil {
		return "canceled", ""
	}

	var params StopAgentParams
	_ = json.Unmarshal([]byte(arguments), &params)
	if strings.TrimSpace(params.Name) == "" {
		return "error: missing name", ""
	}

	// Find which daemon has this agent
	daemonName, err := FindAgentDaemon(ctx, params.Name)
	if err != nil {
		return fmt.Sprintf("error: %v", err), ""
	}

	// Send stop request to the appropriate daemon
	respb, err := ipcRequestToDaemon(ctx, daemonName, struct {
		Type      string `json:"type"`
		AgentName string `json:"agent_name"`
	}{Type: "stop", AgentName: params.Name})
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

	meta := StopAgentMetadata{Name: params.Name, Action: "stop", At: time.Now().Format(time.RFC3339)}
	mb, _ := json.Marshal(meta)

	// Include daemon info in response
	daemonSuffix := ""
	if daemonName != "local" {
		daemonSuffix = fmt.Sprintf(" on daemon %q", daemonName)
	}
	return fmt.Sprintf("Stopped agent %q%s", params.Name, daemonSuffix), string(mb)
}
