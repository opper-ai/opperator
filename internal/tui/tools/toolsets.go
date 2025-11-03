package tools

import (
	"fmt"
	"strings"
)

func OpperatorSpecs() []Spec {
	return []Spec{
		ListAgentsSpec(),
		StartAgentSpec(),
		StopAgentSpec(),
		RestartAgentSpec(),
		GetLogsSpec(),
	}
}

func BuilderSpecs() []Spec {
	return []Spec{
		FocusAgentSpec(),
		PlanSpec(),
		BootstrapNewAgentSpec(),
		ListAgentsSpec(),
		StartAgentSpec(),
		StopAgentSpec(),
		RestartAgentSpec(),
		GetLogsSpec(),
		MoveAgentSpec(),
		ListSecretsSpec(),
		ManageSecretSpec(),
		ViewSpec(),
		LSSpec(),
		WriteSpec(),
		EditSpec(),
		MultiEditSpec(),
		GlobSpec(),
		GrepSpec(),
		RGSpec(),
		DiagnosticsSpec(),
		BashSpec(),
		ReadDocumentationSpec(),
	}
}

type AgentOption struct {
	Value       string
	Label       string
	Description string
}

func AgentSpec(options []AgentOption) Spec {
	opts := options

	values := make([]string, 0, len(opts))
	descriptionBlocks := make([]string, 0, len(opts))

	for _, opt := range opts {
		value := strings.TrimSpace(opt.Value)
		if value == "" {
			continue
		}
		label := strings.TrimSpace(opt.Label)
		if label == "" {
			label = value
		}
		values = append(values, value)
		desc := strings.TrimSpace(opt.Description)
		if desc != "" {
			descriptionBlocks = append(descriptionBlocks, fmt.Sprintf("%s\n%s", label, desc))
		} else {
			descriptionBlocks = append(descriptionBlocks, label)
		}
	}

	agentProp := map[string]any{"type": "string"}
	var descBuilder strings.Builder
	descBuilder.WriteString("Optional name of the managed sub-agent to run.")
	if len(descriptionBlocks) > 0 {
		descBuilder.WriteString("\n\nOptions:\n")
		descBuilder.WriteString(strings.Join(descriptionBlocks, "\n\n"))
	}
	agentProp["description"] = descBuilder.String()
	if len(values) > 0 {
		agentProp["enum"] = values
	}

	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt":          map[string]any{"type": "string", "description": "Prompt for the helper agent"},
			"task_definition": map[string]any{"type": "string", "description": "Very short task definition to be displayed in the TUI"},
			"agent":           agentProp,
		},
		"required": []string{"prompt", "task_definition"},
	}

	return Spec{
		Name:        "agent",
		Description: "Launch a short-lived helper agent. Provide a prompt, task definition, and optionally pick a managed agent via the `agent` parameter.",
		Parameters:  params,
	}
}
