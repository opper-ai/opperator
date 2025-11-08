package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"tui/internal/protocol"
)

const (
	// AgentCommandToolName is the tool identifier for direct agent command invocation.
	AgentCommandToolName = "agent_command"
	agentCommandPrefix   = "agent_command__"
	agentCommandDelay    = 200 * time.Millisecond
)

type ExternalAgentCommandTarget struct {
	Agent         string
	Command       string
	Label         string
	Arguments     []protocol.CommandArgument
	Async         bool
	ProgressLabel string
	Hidden        bool
}

type externalAgentCommandDef struct {
	ToolName      string
	AgentName     string
	Command       string
	Label         string
	Arguments     []protocol.CommandArgument
	Async         bool
	ProgressLabel string
	Hidden        bool
}

var (
	agentCommandMu       sync.RWMutex
	agentCommandRegistry = map[string]ExternalAgentCommandTarget{}
)

// ResetAgentCommandTools clears the cached per-command tool mappings.
func ResetAgentCommandTools() {
	agentCommandMu.Lock()
	defer agentCommandMu.Unlock()
	agentCommandRegistry = map[string]ExternalAgentCommandTarget{}
}

// BuildAgentCommandTools constructs one tool definition per agent command and
// records the mapping so tool executions can be routed to the correct command.
func BuildAgentCommandTools(agent string, commands []protocol.CommandDescriptor) []Spec {
	trimmedAgent := strings.TrimSpace(agent)
	if trimmedAgent == "" || len(commands) == 0 {
		return nil
	}

	sanitizedAgent := sanitizeToolSegment(trimmedAgent)
	if sanitizedAgent == "" {
		sanitizedAgent = "agent"
	}

	normalized := protocol.NormalizeCommandDescriptors(commands)
	defs := make([]externalAgentCommandDef, 0, len(normalized))
	toolsSpecs := make([]Spec, 0, len(normalized))
	used := make(map[string]int)

	for _, cmd := range normalized {
		if !hasAgentToolExposure(cmd) {
			continue
		}

		original := strings.TrimSpace(cmd.Name)
		if original == "" {
			continue
		}

		label := strings.TrimSpace(cmd.Title)
		if label == "" {
			label = original
		}

		sanitized := sanitizeToolSegment(original)
		if sanitized == "" {
			sanitized = "command"
		}
		used[sanitized]++
		suffix := sanitized
		if count := used[sanitized]; count > 1 {
			suffix = fmt.Sprintf("%s_%d", sanitized, count)
		}

		toolName := fmt.Sprintf("%s%s__%s", agentCommandPrefix, sanitizedAgent, suffix)
		defs = append(defs, externalAgentCommandDef{
			ToolName:      toolName,
			AgentName:     trimmedAgent,
			Command:       original,
			Label:         label,
			Arguments:     cmd.Arguments,
			Async:         cmd.Async,
			ProgressLabel: strings.TrimSpace(cmd.ProgressLabel),
			Hidden:        cmd.Hidden,
		})

		description := strings.TrimSpace(cmd.Description)
		if description == "" {
			description = fmt.Sprintf("Execute %s on sub-agent %s", label, trimmedAgent)
		}
		params := buildAgentCommandParameters(cmd)

		toolsSpecs = append(toolsSpecs, Spec{
			Name:        toolName,
			Description: description,
			Parameters:  params,
		})
	}

	setAgentCommandRegistry(defs)
	for _, def := range defs {
		if def.Async {
			registerExternalAgentCommandAsyncRenderer(def)
		} else {
			registerExternalAgentCommandRenderer(def)
		}
	}
	return toolsSpecs
}

func setAgentCommandRegistry(defs []externalAgentCommandDef) {
	agentCommandMu.Lock()
	defer agentCommandMu.Unlock()
	if len(defs) == 0 {
		return
	}
	if agentCommandRegistry == nil {
		agentCommandRegistry = make(map[string]ExternalAgentCommandTarget)
	}
	for _, def := range defs {
		agentCommandRegistry[def.ToolName] = ExternalAgentCommandTarget{
			Agent:         def.AgentName,
			Command:       def.Command,
			Label:         def.Label,
			Arguments:     def.Arguments,
			Async:         def.Async,
			ProgressLabel: def.ProgressLabel,
			Hidden:        def.Hidden,
		}
	}
}

// LookupAgentCommandTool retrieves the target command metadata for a generated agent command tool.
func LookupAgentCommandTool(name string) (ExternalAgentCommandTarget, bool) {
	agentCommandMu.RLock()
	defer agentCommandMu.RUnlock()
	target, ok := agentCommandRegistry[name]
	return target, ok
}

// LookupAgentCommandToolName finds the generated tool name for a given agent and command.
func LookupAgentCommandToolName(agent, command string) (string, bool) {
	agentCommandMu.RLock()
	defer agentCommandMu.RUnlock()
	trimmedAgent := strings.TrimSpace(agent)
	trimmedCommand := strings.TrimSpace(command)
	for toolName, target := range agentCommandRegistry {
		if strings.EqualFold(strings.TrimSpace(target.Agent), trimmedAgent) &&
			strings.EqualFold(strings.TrimSpace(target.Command), trimmedCommand) {
			return toolName, true
		}
	}
	return "", false
}

// IsAgentCommandToolName reports whether name corresponds to a generated agent command tool.
func IsAgentCommandToolName(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if lower == AgentCommandToolName {
		return true
	}
	return strings.HasPrefix(lower, agentCommandPrefix)
}

func extractCommandArgs(arguments string, schema []protocol.CommandArgument) (map[string]any, error) {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" || trimmed == "null" {
		return map[string]any{}, nil
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return nil, err
	}

	return parsed, nil
}

func buildAgentCommandParameters(cmd protocol.CommandDescriptor) map[string]any {
	base := map[string]any{
		"type": "object",
	}

	properties := make(map[string]any)
	required := make([]string, 0, len(cmd.Arguments))

	for _, arg := range cmd.Arguments {
		name := strings.TrimSpace(arg.Name)
		if name == "" {
			continue
		}

		typeName := strings.TrimSpace(arg.Type)
		if typeName == "" {
			typeName = "string"
		}

		argSchema := map[string]any{
			"type": typeName,
		}
		if desc := strings.TrimSpace(arg.Description); desc != "" {
			argSchema["description"] = desc
		}
		if arg.Default != nil {
			argSchema["default"] = arg.Default
		}
		if len(arg.Enum) > 0 {
			argSchema["enum"] = arg.Enum
		}
		if arg.Items != nil {
			argSchema["items"] = arg.Items
		}
		if arg.Properties != nil {
			argSchema["properties"] = arg.Properties
		}

		properties[name] = argSchema
		if arg.Required {
			required = append(required, name)
		}
	}

	base["properties"] = properties
	if len(required) > 0 {
		base["required"] = required
	}
	return base
}

func sanitizeToolSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteByte('_')
		}
	}
	sanitized := strings.Trim(b.String(), "_")
	return sanitized
}

func hasAgentToolExposure(cmd protocol.CommandDescriptor) bool {
	for _, exposure := range cmd.ExposeAs {
		if exposure == protocol.CommandExposureAgentTool {
			return true
		}
	}
	return len(cmd.ExposeAs) == 0
}

// RunAgentCommand executes the IPC call associated with a generated agent command tool.
func RunAgentCommand(ctx context.Context, toolName, arguments, workingDir string) (content, metadata string) {
	if err := SleepWithCancel(ctx, agentCommandDelay); err != nil {
		return "canceled", ""
	}

	arguments = strings.TrimSpace(arguments)
	workingDir = strings.TrimSpace(workingDir)

	var (
		agentName   string
		commandName string
		argsData    map[string]any
	)

	if target, ok := LookupAgentCommandTool(toolName); ok {
		agentName = strings.TrimSpace(target.Agent)
		commandName = strings.TrimSpace(target.Command)
		commandLabel := strings.TrimSpace(target.Label)
		parsedArgs, err := extractCommandArgs(arguments, target.Arguments)
		if err != nil {
			return fmt.Sprintf("error parsing parameters: %v", err), ""
		}
		argsData = parsedArgs
		if target.Async {
			sessionID := SessionIDFromContext(ctx)
			callID := CallIDFromContext(ctx)
			payload := map[string]any{
				"tool":    commandName,
				"input":   argsData,
				"mode":    "agent",
				"agent":   agentName,
				"command": commandName,
			}
			if commandLabel != "" {
				payload["command_label"] = commandLabel
			}
			if len(argsData) > 0 {
				if raw, err := json.Marshal(argsData); err == nil {
					payload["command_args"] = json.RawMessage(raw)
				}
			}
			if trimmed := strings.TrimSpace(target.ProgressLabel); trimmed != "" {
				payload["progress_label"] = trimmed
			}
			body, err := json.Marshal(payload)
			if err != nil {
				return fmt.Sprintf("error preparing async command: %v", err), ""
			}
			return RunAsyncTool(ctx, string(body), workingDir, sessionID, callID)
		}
	}

	if agentName == "" || commandName == "" {
		return "error: unknown agent command tool", ""
	}

	// Find which daemon has this agent
	daemonName, err := FindAgentDaemon(ctx, agentName)
	if err != nil {
		return fmt.Sprintf("error: %v", err), ""
	}

	payload := struct {
		Type       string         `json:"type"`
		AgentName  string         `json:"agent_name"`
		Command    string         `json:"command"`
		Args       map[string]any `json:"args,omitempty"`
		WorkingDir string         `json:"working_dir,omitempty"`
	}{
		Type:       "command",
		AgentName:  agentName,
		Command:    commandName,
		Args:       argsData,
		WorkingDir: workingDir,
	}
	// Send command to the correct daemon
	respb, err := IPCRequestToDaemon(ctx, daemonName, payload)
	if err != nil {
		return fmt.Sprintf("error: %v", err), ""
	}
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Command struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
			Result  any    `json:"result"`
		} `json:"command"`
	}
	if err := json.Unmarshal(respb, &resp); err != nil {
		return fmt.Sprintf("error decoding response: %v", err), ""
	}
	if !resp.Success {
		if resp.Error == "" {
			resp.Error = "unknown error"
		}
		return "error: " + resp.Error, ""
	}
	if !resp.Command.Success {
		errMsg := resp.Command.Error
		if errMsg == "" {
			errMsg = "command failed"
		}
		meta := map[string]any{
			"agent":   agentName,
			"command": commandName,
			"success": false,
			"error":   errMsg,
		}
		if mb, err := json.Marshal(meta); err == nil {
			metadata = string(mb)
		}
		return "error: " + errMsg, metadata
	}
	result := "command succeeded"
	if resp.Command.Result != nil {
		if b, err := json.MarshalIndent(resp.Command.Result, "", "  "); err == nil {
			result = string(b)
		}
	}
	meta := map[string]any{
		"agent":   agentName,
		"command": commandName,
		"success": true,
		"result":  resp.Command.Result,
	}
	if mb, err := json.Marshal(meta); err == nil {
		metadata = string(mb)
	}
	return result, metadata
}
