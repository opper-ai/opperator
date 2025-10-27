package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"tui/internal/pubsub"
)

//go:embed focus_agent.md
var focusAgentDescription []byte

const (
	FocusAgentToolName = "focus_agent"
	focusAgentDelay    = 1 * time.Millisecond
)

type FocusAgentEvent struct {
	AgentName string
}

var focusAgentBroker = pubsub.NewBroker[FocusAgentEvent]()

type FocusAgentParams struct {
	AgentName string `json:"agent_name"`
}

type FocusAgentMetadata struct {
	AgentName string `json:"agent_name"`
	Action    string `json:"action"`
	At        string `json:"at"`
}

func FocusAgentSpec() Spec {
	return Spec{
		Name:        FocusAgentToolName,
		Description: strings.TrimSpace(string(focusAgentDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_name": map[string]any{
					"type":        "string",
					"description": "Name of the agent to focus on. Leave empty to clear focus.",
				},
			},
		},
	}
}

func RunFocusAgent(ctx context.Context, arguments string) (string, string) {
	if err := sleepWithCancel(ctx, focusAgentDelay); err != nil {
		return "canceled", ""
	}

	var params FocusAgentParams
	_ = json.Unmarshal([]byte(arguments), &params)

	agentName := strings.TrimSpace(params.AgentName)

	// If not clearing focus, validate that the agent exists
	if agentName != "" {
		respb, err := ipcRequestCtx(ctx, struct {
			Type string `json:"type"`
		}{Type: "list"})
		if err != nil {
			return fmt.Sprintf("error: %v", err), ""
		}

		var resp struct {
			Success   bool   `json:"success"`
			Error     string `json:"error"`
			Processes []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"processes"`
		}
		if err := json.Unmarshal(respb, &resp); err != nil {
			return fmt.Sprintf("error decoding response: %v", err), ""
		}

		if !resp.Success {
			return fmt.Sprintf("error: %s", resp.Error), ""
		}

		// Check if agent exists
		found := false
		for _, p := range resp.Processes {
			if p.Name == agentName {
				found = true
				break
			}
		}

		if !found {
			return fmt.Sprintf("error: agent %q does not exist", agentName), ""
		}
	}

	// Publish event to update the focused agent in the UI
	PublishFocusAgentEvent(agentName)

	var message string
	var action string
	if agentName == "" {
		message = "Cleared focused agent"
		action = "clear"
	} else {
		message = fmt.Sprintf("Focused on agent %q", agentName)
		action = "focus"
	}

	meta := FocusAgentMetadata{
		AgentName: agentName,
		Action:    action,
		At:        time.Now().Format(time.RFC3339),
	}
	mb, _ := json.Marshal(meta)

	return message, string(mb)
}

// PublishFocusAgentEvent publishes a focus agent event to update the UI
func PublishFocusAgentEvent(agentName string) {
	focusAgentBroker.Publish(pubsub.UpdatedEvent, FocusAgentEvent{
		AgentName: agentName,
	})
}

func SubscribeFocusAgentEvents(ctx context.Context) <-chan pubsub.Event[FocusAgentEvent] {
	return focusAgentBroker.Subscribe(ctx)
}
