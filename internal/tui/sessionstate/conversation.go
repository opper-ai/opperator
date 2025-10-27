package sessionstate

import (
	"encoding/json"
	"fmt"
	"strings"

	"tui/coreagent"
	tooling "tui/tools"
	tooltypes "tui/tools/types"
)

type AgentOption struct {
	Name        string
	Status      string
	Description string
}

type FocusedAgentInfo struct {
	Name        string
	Description string
	Todos       []TodoInfo
}

type TodoInfo struct {
	Text      string
	Completed bool
}

// BuildInstructions constructs the instruction payload for a session request.
func BuildInstructions(basePrompt, agentName, agentPrompt string, agentOptions []AgentOption, agentListErr error, focusedAgentTools []tooling.Spec, focusedAgentInfo FocusedAgentInfo, coreAgentID string) string {
	base := strings.TrimSpace(basePrompt)
	if base == "" {
		base = coreagent.Default().Prompt
	}
	listSection := strings.TrimSpace(agentListInstructions(agentOptions, agentListErr))
	trimmedAgent := strings.TrimSpace(agentName)
	if trimmedAgent != "" {
		var b strings.Builder
		opperatorPrompt := strings.TrimSpace(coreagent.Default().Prompt)
		if opperatorPrompt == "" {
			opperatorPrompt = base
		}
		b.WriteString(opperatorPrompt)
		if listSection != "" {
			b.WriteString("\n\n")
			b.WriteString(listSection)
		}
		b.WriteString("\n\nYou are currently interacting directly with the managed agent '")
		b.WriteString(trimmedAgent)
		b.WriteString("'. Use the available command tools to operate it. If arguments are required, construct valid JSON objects in the tool call.")
		if trimmedPrompt := strings.TrimSpace(agentPrompt); trimmedPrompt != "" {
			b.WriteString("\n\nSub-agent instructions:\n")
			b.WriteString(trimmedPrompt)
			b.WriteString("\n\nImportant:\nPlace priority on following these sub-agent instructions over any previous instructions.\n")
		}
		return b.String()
	}

	// Add focused agent tools section for Builder when tools are available
	toolsSection := ""
	docsSection := ""
	isBuilder := strings.EqualFold(strings.TrimSpace(coreAgentID), coreagent.IDBuilder)
	if isBuilder {
		if len(focusedAgentTools) > 0 {
			toolsSection = formatFocusedAgentTools(focusedAgentTools)
		} else if strings.TrimSpace(focusedAgentInfo.Name) != "" {
			// Agent is focused but not running - show spec and todos
			toolsSection = formatFocusedAgentInfo(focusedAgentInfo)
		} else {
			// No agent is focused
			toolsSection = "## Warning\n\nYou are not currently focused on any agent. Use the `focus_agent` tool to focus on an agent before attempting to interact with it."
		}
		// Add available documentation section for Builder
		docsSection = formatAvailableDocuments()
	}

	// Only show agent list for non-Builder agents (Builder doesn't have the agent tool)
	includeAgentList := !isBuilder
	if (includeAgentList && listSection != "") || toolsSection != "" || docsSection != "" {
		var b strings.Builder
		b.WriteString(base)
		if docsSection != "" {
			b.WriteString("\n\n")
			b.WriteString(docsSection)
		}
		if includeAgentList && listSection != "" {
			b.WriteString("\n\n")
			b.WriteString(listSection)
		}
		if toolsSection != "" {
			b.WriteString("\n\n")
			b.WriteString(toolsSection)
		}

		// Add Builder response format reminder at the end
		if isBuilder {
			b.WriteString("\n\nIMPORTANT: ALWAYS RESPOND AS `{\"text\": \"...\", \"tools\": [...]}`.")
		}

		return b.String()
	}

	// For Builder with no tools/sections, still add the reminder and docs
	if isBuilder {
		var b strings.Builder
		b.WriteString(base)
		if docsSection != "" {
			b.WriteString("\n\n")
			b.WriteString(docsSection)
		}
		b.WriteString("\n\nIMPORTANT: ALWAYS RESPOND AS `{\"text\": \"...\", \"tools\": [...]}`.")
		return b.String()
	}

	return base
}

// BuildConversation converts history into the LLM conversation format.
func BuildConversation(history []Message) []map[string]any {
	if len(history) == 0 {
		return nil
	}
	conv := make([]map[string]any, 0, len(history))
	for _, h := range history {
		conv = append(conv, messageToConversationEntries(h)...)
	}
	return conv
}

func messageToConversationEntries(h Message) []map[string]any {
	role := strings.ToLower(strings.TrimSpace(h.Role))
	entries := make([]map[string]any, 0, 1+len(h.ToolResults))

	switch role {
	case "user":
		if content := strings.TrimSpace(h.Content); content != "" {
			entries = append(entries, map[string]any{"role": "user", "content": content})
		}
	case "assistant":
		entry := map[string]any{"role": "assistant"}
		if strings.TrimSpace(h.Content) != "" {
			entry["content"] = h.Content
		}
		if len(h.ToolCalls) > 0 {
			entry["tool_calls"] = convertToolCallsForConversation(h.ToolCalls)
		}
		if len(entry) > 1 || len(h.ToolCalls) > 0 {
			entries = append(entries, entry)
		}
	default:
		if strings.TrimSpace(h.Content) != "" {
			entries = append(entries, map[string]any{"role": role, "content": h.Content})
		}
	}

	if len(h.ToolResults) > 0 {
		for _, r := range h.ToolResults {
			msg := map[string]any{
				"role":    "tool_call_output",
				"tool_id": r.ToolCallID,
			}
			if strings.TrimSpace(r.Name) != "" {
				msg["name"] = r.Name
			}
			if strings.TrimSpace(r.Content) != "" {
				msg["content"] = r.Content
			}
			if strings.TrimSpace(r.Metadata) != "" {
				msg["metadata"] = r.Metadata
			}
			if r.IsError {
				msg["is_error"] = true
			}
			entries = append(entries, msg)
		}
	}

	return entries
}

func convertToolCallsForConversation(calls []tooltypes.Call) []map[string]any {
	if len(calls) == 0 {
		return nil
	}
	entries := make([]map[string]any, 0, len(calls))
	for _, c := range calls {
		entry := map[string]any{
			"id":   c.ID,
			"type": "function",
			"function": map[string]any{
				"name": c.Name,
				"arguments": func() string {
					if strings.TrimSpace(c.Input) == "" {
						return "{}"
					}
					return c.Input
				}(),
			},
		}
		if strings.TrimSpace(c.Reason) != "" {
			entry["reason"] = c.Reason
		}
		entries = append(entries, entry)
	}
	return entries
}

func agentOptionDescriptor(description, status string) string {
	description = strings.TrimSpace(description)
	status = strings.TrimSpace(status)
	switch {
	case description == "" && status == "":
		return ""
	case status == "":
		return description
	case description == "":
		return status
	default:
		return description + " — " + status
	}
}

func agentListInstructions(options []AgentOption, listErr error) string {
	blocks := make([]string, 0, len(options)+1)
	builderLabel := "Builder"
	if def, ok := coreagent.Lookup(coreagent.IDBuilder); ok {
		if trimmed := strings.TrimSpace(def.Name); trimmed != "" {
			builderLabel = trimmed
		}
	}
	builderBlock := agentDescriptorBlock(builderLabel, "Built-in helper agent with access to project tools.", "running")
	if builderBlock != "" {
		blocks = append(blocks, builderBlock)
	}
	for _, opt := range options {
		block := agentDescriptorBlock(opt.Name, opt.Description, opt.Status)
		if block == "" {
			continue
		}
		blocks = append(blocks, block)
	}

	var b strings.Builder
	b.WriteString("Available managed sub-agents for the `agent` tool (set the `agent` parameter to one of these values):\n")
	if len(blocks) == 0 {
		if listErr != nil {
			b.WriteString("\n(managed agent list unavailable; see warning below)")
		} else {
			b.WriteString("\n(no managed sub-agents detected)")
		}
	} else {
		for i, block := range blocks {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(block)
		}
	}
	if listErr != nil {
		b.WriteString("\n\nWarning: failed to refresh the managed agent list — ")
		b.WriteString(strings.TrimSpace(listErr.Error()))
		b.WriteString(". Use local tools if unsure.")
	}
	return b.String()
}

func agentDescriptorBlock(name, description, status string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	desc := agentOptionDescriptor(description, status)
	return fmt.Sprintf("%s\n%s", trimmed, desc)
}

func agentStatusMessage(status string) (string, string) {
	value := strings.ToLower(strings.TrimSpace(status))
	switch value {
	case "running", "ready":
		return "Online", ""
	case "stopped":
		return "Offline", ""
	case "error":
		return "Error", "Check agent logs"
	default:
		return strings.Title(value), ""
	}
}

// formatFocusedAgentTools creates a formatted section showing available focused agent tools
func formatFocusedAgentTools(tools []tooling.Spec) string {
	if len(tools) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("# Focused Agent\n\n")
	b.WriteString("## Currently Focused Agent Tools\n\n")
	b.WriteString("You currently have access to the following tools from the focused agent:\n\n")

	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}

		b.WriteString("### ")
		b.WriteString(name)
		b.WriteString("\n")

		if desc := strings.TrimSpace(tool.Description); desc != "" {
			b.WriteString(desc)
			b.WriteString("\n")
		}

		if tool.Parameters != nil {
			// Format parameters
			if params := formatToolParameters(tool.Parameters); params != "" {
				b.WriteString("\nParameters:\n")
				b.WriteString(params)
			}
		}

		b.WriteString("\n")
	}

	return b.String()
}

// formatFocusedAgentInfo formats focused agent info when the agent is not running
func formatFocusedAgentInfo(info FocusedAgentInfo) string {
	var b strings.Builder

	b.WriteString("# Focused Agent\n\n")

	b.WriteString("## Warning\n\n")
	b.WriteString("You are focused on agent '")
	b.WriteString(info.Name)
	b.WriteString("', but it is not currently running. Start the agent to interact with it.\n\n")

	// Add specification if available
	if spec := strings.TrimSpace(info.Description); spec != "" {
		b.WriteString("You have the following specification:\n\n")
		b.WriteString(spec)
		b.WriteString("\n\n")
	}

	// Add todo list if available
	if len(info.Todos) > 0 {
		b.WriteString("Todo list:\n")
		for _, todo := range info.Todos {
			if todo.Completed {
				b.WriteString("[X] ")
			} else {
				b.WriteString("[ ] ")
			}
			b.WriteString(todo.Text)
			b.WriteString("\n")
		}
	} else {
		b.WriteString("Your todo list is currently empty.\n")
	}

	return b.String()
}

// formatAvailableDocuments creates a formatted section showing available documentation
func formatAvailableDocuments() string {
	docs, err := tooling.GetAvailableDocuments()
	if err != nil || len(docs) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("### Available Documentation\n\n")

	for _, doc := range docs {
		b.WriteString("## ")
		b.WriteString(doc.Name)
		b.WriteString("\n")
		b.WriteString(doc.Description)
		b.WriteString("\n\n")
	}

	return b.String()
}

// formatToolParameters formats the parameters object into a readable string
func formatToolParameters(params map[string]any) string {
	if params == nil {
		return ""
	}

	var b strings.Builder

	// Get properties if they exist
	properties, hasProps := params["properties"].(map[string]any)
	required, _ := params["required"].([]string)
	requiredSet := make(map[string]bool)
	for _, r := range required {
		requiredSet[r] = true
	}

	if !hasProps || len(properties) == 0 {
		// Try to format the whole params object as JSON
		if jsonBytes, err := json.MarshalIndent(params, "", "  "); err == nil {
			b.WriteString("```json\n")
			b.WriteString(string(jsonBytes))
			b.WriteString("\n```\n")
		}
		return b.String()
	}

	// Format each property
	for propName, propValue := range properties {
		propMap, ok := propValue.(map[string]any)
		if !ok {
			continue
		}

		b.WriteString("- `")
		b.WriteString(propName)
		b.WriteString("`")

		if requiredSet[propName] {
			b.WriteString(" (required)")
		}

		if propType, ok := propMap["type"].(string); ok {
			b.WriteString(": ")
			b.WriteString(propType)
		}

		if propDesc, ok := propMap["description"].(string); ok && strings.TrimSpace(propDesc) != "" {
			b.WriteString(" - ")
			b.WriteString(strings.TrimSpace(propDesc))
		}

		b.WriteString("\n")
	}

	return b.String()
}
