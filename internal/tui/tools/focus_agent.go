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

	// If not clearing focus, validate that the agent exists and is local
	if agentName != "" {
		// Use FindAgentDaemon to determine which daemon the agent belongs to
		daemonName, err := FindAgentDaemon(ctx, agentName)
		if err != nil {
			return fmt.Sprintf("error: %v", err), ""
		}

		// Reject non-local agents
		if daemonName != "local" {
			return fmt.Sprintf("error: cannot focus on cloud agent %q (on daemon @%s). The builder can only focus on local agents.", agentName, daemonName), ""
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
