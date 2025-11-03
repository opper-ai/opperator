package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

//go:embed move_agent.md
var moveAgentDescription []byte

const (
	MoveAgentToolName = "move_agent"
	moveAgentDelay    = 1 * time.Millisecond
)

type MoveAgentParams struct {
	AgentName string `json:"agent_name"`
}

type MoveAgentMetadata struct {
	AgentName    string `json:"agent_name"`
	SourceDaemon string `json:"source_daemon"`
	TargetDaemon string `json:"target_daemon"`
	Action       string `json:"action"`
	At           string `json:"at"`
}

func MoveAgentSpec() Spec {
	return Spec{
		Name:        MoveAgentToolName,
		Description: strings.TrimSpace(string(moveAgentDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_name": map[string]any{
					"type":        "string",
					"description": "Name of the agent to move from cloud/remote daemon to local",
				},
			},
			"required": []string{"agent_name"},
		},
	}
}

func RunMoveAgent(ctx context.Context, arguments string) (string, string) {
	if err := sleepWithCancel(ctx, moveAgentDelay); err != nil {
		return "canceled", ""
	}

	var params MoveAgentParams
	_ = json.Unmarshal([]byte(arguments), &params)

	agentName := strings.TrimSpace(params.AgentName)
	if agentName == "" {
		return "error: missing agent_name", ""
	}

	// Find which daemon the agent currently belongs to
	sourceDaemon, err := FindAgentDaemon(ctx, agentName)
	if err != nil {
		return fmt.Sprintf("error: %v", err), ""
	}

	// Validate direction: only allow cloud-to-local moves
	if sourceDaemon == "local" {
		return fmt.Sprintf("error: agent %q is already on local daemon. Only cloud-to-local moves are supported.", agentName), ""
	}

	// Target is always local
	targetDaemon := "local"

	// Hardcoded safe defaults
	force := false
	noStart := false

	// Step 1: Package the agent from the source (cloud) daemon
	packageResp, err := ipcRequestToDaemon(ctx, sourceDaemon, struct {
		Type      string `json:"type"`
		AgentName string `json:"agent_name"`
	}{Type: "package_agent", AgentName: agentName})
	if err != nil {
		return fmt.Sprintf("error: failed to package agent: %v", err), ""
	}

	var pkgResp struct {
		Success      bool   `json:"success"`
		Error        string `json:"error"`
		AgentPackage any    `json:"agent_package"` // Keep as any since tui can't import agent.AgentPackage
	}
	if err := json.Unmarshal(packageResp, &pkgResp); err != nil {
		return fmt.Sprintf("error: failed to parse package response: %v", err), ""
	}

	if !pkgResp.Success {
		errMsg := strings.TrimSpace(pkgResp.Error)
		if errMsg == "" {
			errMsg = "failed to package agent"
		}
		return fmt.Sprintf("error: %s", errMsg), ""
	}

	// Step 2: Send the packaged agent to local daemon
	receiveResp, err := ipcRequestToDaemon(ctx, targetDaemon, struct {
		Type         string `json:"type"`
		AgentPackage any    `json:"agent_package"`
		Force        bool   `json:"force"`
		StartAfter   bool   `json:"start_after"`
	}{
		Type:         "receive_agent",
		AgentPackage: pkgResp.AgentPackage,
		Force:        force,
		StartAfter:   !noStart,
	})
	if err != nil {
		return fmt.Sprintf("error: failed to send to local daemon: %v", err), ""
	}

	var recvResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(receiveResp, &recvResp); err != nil {
		return fmt.Sprintf("error: failed to parse receive response: %v", err), ""
	}

	if !recvResp.Success {
		errMsg := strings.TrimSpace(recvResp.Error)
		if errMsg == "" {
			errMsg = "failed to receive agent on local daemon"
		}
		return fmt.Sprintf("error: %s", errMsg), ""
	}

	// Step 3: Delete the agent from source (cloud) daemon
	deleteResp, err := ipcRequestToDaemon(ctx, sourceDaemon, struct {
		Type      string `json:"type"`
		AgentName string `json:"agent_name"`
	}{Type: "delete_agent", AgentName: agentName})
	if err != nil {
		// Non-fatal - agent was already transferred
		// Just log it but don't fail
	} else {
		var delResp struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal(deleteResp, &delResp); err == nil && !delResp.Success {
			// Non-fatal warning
		}
	}

	// Success metadata
	meta := MoveAgentMetadata{
		AgentName:    agentName,
		SourceDaemon: sourceDaemon,
		TargetDaemon: targetDaemon,
		Action:       "move",
		At:           time.Now().Format(time.RFC3339),
	}
	mb, _ := json.Marshal(meta)

	return fmt.Sprintf("Successfully moved agent %q from daemon @%s to local. Agent has been started.", agentName, sourceDaemon), string(mb)
}
