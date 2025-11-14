package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"tui/internal/keyring"
	"tui/opper"
	"tui/lsp"
	"tui/permission"
	"tui/secretprompt"
	tooling "tui/tools"
)

type toolRunner interface {
	Execute(ctx context.Context, name string, args string, progress func(SubAgentEvent)) (content, metadata string)
}

func newToolRunner(perms permission.Service, secrets secretprompt.Service, workingDir string, invocationDir string, manager *lsp.Manager) toolRunner {
	return &localToolRunner{permissions: perms, secrets: secrets, workingDir: workingDir, invocationDir: invocationDir, lsp: manager}
}

type localToolRunner struct {
	permissions   permission.Service
	secrets       secretprompt.Service
	workingDir    string
	invocationDir string
	lsp           *lsp.Manager
}

func (r *localToolRunner) Execute(ctx context.Context, name string, args string, progress func(SubAgentEvent)) (string, string) {
	lower := strings.ToLower(name)
	switch lower {
	case tooling.ViewToolName:
		return tooling.RunView(ctx, args, r.workingDir)
	case tooling.LSToolName:
		return tooling.RunLS(ctx, args, r.workingDir)
	case tooling.WriteToolName:
		return tooling.RunWrite(ctx, args, r.workingDir)
	case tooling.EditToolName:
		return tooling.RunEdit(ctx, args, r.workingDir)
	case tooling.MultiEditToolName:
		return tooling.RunMultiEdit(ctx, args, r.workingDir)
	case tooling.GlobToolName:
		return tooling.RunGlob(ctx, args, r.workingDir)
	case tooling.GrepToolName:
		return tooling.RunGrep(ctx, args, r.workingDir)
	case tooling.RGToolName:
		return tooling.RunRG(ctx, args, r.workingDir)
	case tooling.DiagnosticsToolName:
		return tooling.RunDiagnostics(ctx, args, r.workingDir, r.lsp)
	case tooling.BashToolName:
		return tooling.RunBash(ctx, args, r.workingDir)
	case tooling.AsyncToolName:
		sessionID := tooling.SessionIDFromContext(ctx)
		callID := tooling.CallIDFromContext(ctx)
		return tooling.RunAsyncTool(ctx, args, r.workingDir, sessionID, callID)
	case tooling.ListAgentsToolName:
		return tooling.RunListAgents(ctx, args)
	case tooling.StartAgentToolName:
		return tooling.RunStartAgent(ctx, args)
	case tooling.StopAgentToolName:
		return tooling.RunStopAgent(ctx, args)
	case tooling.RestartAgentToolName:
		return tooling.RunRestartAgent(ctx, args)
	case tooling.GetLogsToolName:
		return tooling.RunGetLogs(ctx, args)
	case tooling.MoveAgentToolName:
		return tooling.RunMoveAgent(ctx, args)
	case tooling.ManageSecretToolName:
		return r.runManageSecret(ctx, args)
	case tooling.ListSecretsToolName:
		return tooling.RunListSecrets(ctx, args)
	case tooling.FocusAgentToolName:
		return tooling.RunFocusAgent(ctx, args)
	case tooling.PlanToolName:
		return tooling.RunPlan(ctx, args)
	case tooling.BootstrapNewAgentToolName:
		return tooling.RunBootstrapNewAgent(ctx, args)
	case tooling.ReadDocumentationToolName:
		return tooling.RunReadDocumentation(ctx, args)
	case "agent":
		// Block Builder from executing the agent tool
		activeAgent := tooling.ActiveAgentFromContext(ctx)
		coreAgent := tooling.CoreAgentFromContext(ctx)
		if strings.TrimSpace(activeAgent) == "" && strings.EqualFold(strings.TrimSpace(coreAgent), "builder") {
			return "error: The Builder agent cannot spawn nested agents. Use agent management tools (start_agent, stop_agent, focus_agent) instead.", ""
		}
		return runLocalAgentToolProgressive(ctx, args, progress, r.permissions, r.secrets, r.workingDir, r.invocationDir)
	case tooling.AgentCommandToolName:
		return tooling.RunAgentCommand(ctx, name, args, r.invocationDir)
	}

	if _, ok := tooling.LookupAgentCommandTool(name); ok {
		return tooling.RunAgentCommand(ctx, name, args, r.invocationDir)
	}

	return fmt.Sprintf("unknown tool: %s", name), ""
}

func (r *localToolRunner) runManageSecret(ctx context.Context, args string) (string, string) {
	params, err := tooling.ParseManageSecretParams(args)
	if err != nil {
		return fmt.Sprintf("error parsing parameters: %v", err), ""
	}

	mode := strings.ToLower(params.Mode)
	if mode == "delete" {
		return tooling.RunManageSecret(ctx, args)
	}

	if params.Name == "" {
		return "error: secret name is required", ""
	}

	defaultValue := params.Value
	errorMsg := ""
	const maxAttempts = 3
	sessionID := tooling.SessionIDFromContext(ctx)
	callID := tooling.CallIDFromContext(ctx)
	title := params.Title
	if title == "" {
		switch mode {
		case "update":
			title = fmt.Sprintf("Update secret %s", params.Name)
		case "delete":
			title = fmt.Sprintf("Delete secret %s", params.Name)
		default:
			title = fmt.Sprintf("Store secret %s", params.Name)
		}
	}
	label := params.ValueLabel
	if label == "" {
		label = "Secret value"
	}

	var lastResult, lastMetadata string

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if params.Value == "" {
			if r.secrets == nil {
				return "error: secret prompt service unavailable", ""
			}
			promptCtx, cancel := context.WithCancel(ctx)
			value, promptErr := r.secrets.Request(promptCtx, secretprompt.CreateRequest{
				SessionID:        sessionID,
				ToolCallID:       callID,
				Name:             params.Name,
				Mode:             mode,
				Title:            title,
				Description:      params.Description,
				ValueLabel:       label,
				DocumentationURL: params.DocumentationURL,
				DefaultValue:     defaultValue,
				Error:            errorMsg,
			})
			cancel()
			if promptErr != nil {
				switch {
				case errors.Is(promptErr, context.Canceled), errors.Is(promptErr, secretprompt.ErrCanceled):
					return "canceled", ""
				default:
					return fmt.Sprintf("error collecting secret: %v", promptErr), ""
				}
			}
			params.Value = strings.TrimSpace(value)
			defaultValue = params.Value
			errorMsg = ""
		}

		cleaned := params.Clean()
		payload, err := json.Marshal(cleaned)
		if err != nil {
			return fmt.Sprintf("error preparing secret payload: %v", err), ""
		}

		result, metadata := tooling.RunManageSecret(ctx, string(payload))
		trimmed := strings.TrimSpace(result)
		lower := strings.ToLower(trimmed)
		if !strings.HasPrefix(lower, "error") {
			return result, metadata
		}

		lastResult = result
		lastMetadata = metadata

		if attempt == maxAttempts-1 {
			return result, metadata
		}

		reason := trimmed
		if colon := strings.Index(trimmed, ":"); colon >= 0 {
			reason = strings.TrimSpace(trimmed[colon+1:])
		}
		errorMsg = reason
		params.Value = ""
	}

	return lastResult, lastMetadata
}

func runLocalAgentToolProgressive(ctx context.Context, arguments string, progress func(SubAgentEvent), perms permission.Service, secrets secretprompt.Service, workingDir string, invocationDir string) (content, metadata string) {
	var payload struct {
		Prompt            string `json:"prompt"`
		TaskDefinition    string `json:"task_definition"`
		AltTaskDefinition string `json:"taskDefinition"`
		AgentName         string `json:"agent"`
		AltAgentName      string `json:"agent_name"`
	}
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return fmt.Sprintf("error parsing parameters: %v", err), ""
	}
	prompt := strings.TrimSpace(payload.Prompt)
	if prompt == "" {
		return "error: missing prompt", ""
	}

	taskDefinition := strings.TrimSpace(payload.TaskDefinition)
	if taskDefinition == "" {
		taskDefinition = strings.TrimSpace(payload.AltTaskDefinition)
	}

	agentParameter := strings.TrimSpace(payload.AgentName)
	if agentParameter == "" {
		agentParameter = strings.TrimSpace(payload.AltAgentName)
	}

	apiKey, err := keyring.GetAPIKey()
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return fmt.Sprintf("error: Opper API key is not configured. Run `op secret create %s` to store one", keyring.OpperAPIKeyName), ""
		}
		return fmt.Sprintf("error: failed to read Opper API key: %v", err), ""
	}

	client := opper.New(apiKey)

	conversation := []map[string]any{{
		"role":    "user",
		"content": prompt,
	}}

	var (
		transcript []SubAgentEvent
		callSeq    int
		runner     = newToolRunner(perms, secrets, workingDir, invocationDir, nil)
	)

	resolvedAgentID := strings.TrimSpace(agentParameter)
	if resolvedAgentID == "" {
		resolvedAgentID = "builder"
	}

	// Block Builder from being invoked as a sub-agent
	if strings.EqualFold(resolvedAgentID, "builder") {
		return "error: The Builder agent cannot be used as a sub-agent. Press Shift+tab to switch to the builder agent.", ""
	}

	var (
		specs        []tooling.Spec
		instructions string
		agentDisplay string
	)

	if strings.EqualFold(resolvedAgentID, "builder") {
		specs = append([]tooling.Spec{}, tooling.BuilderSpecs()...)
		instructions = builderAgentInstructions()
		resolvedAgentID = "builder"
		agentDisplay = "Builder"
	} else {
		metaCtx := ctx
		if metaCtx == nil {
			metaCtx = context.Background()
		}
		meta, err := FetchAgentMetadata(metaCtx, resolvedAgentID)
		if err != nil {
			return fmt.Sprintf("error preparing agent %s: %v", strings.TrimSpace(agentParameter), err), ""
		}
		commandSpecs := tooling.BuildAgentCommandTools(meta.Name, meta.Commands)
		if len(commandSpecs) == 0 {
			return fmt.Sprintf("error: agent %s exposes no commands", meta.Name), ""
		}
		specs = append([]tooling.Spec{}, commandSpecs...)
		agentDisplay = strings.TrimSpace(meta.Name)
		if agentDisplay == "" {
			agentDisplay = resolvedAgentID
		}
		resolvedAgentID = strings.TrimSpace(meta.Name)
		if resolvedAgentID == "" {
			resolvedAgentID = strings.TrimSpace(agentParameter)
		}
		instructions = remoteAgentInstructions(meta.SystemPrompt, meta.SystemPromptReplace, agentDisplay)
	}
	if strings.TrimSpace(instructions) == "" {
		instructions = builderAgentInstructions()
	}

	for pass := 0; pass < maxFollowPasses; pass++ {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return "canceled", ""
			default:
			}
		}

		req := opper.StreamRequest{
			Name:         "opperator.agent_tool",
			Instructions: &instructions,
			Input: map[string]any{
				"conversation": conversation,
				"tools":        tooling.SpecsToAPIDefinitions(specs),
			},
			OutputSchema: sessionOutputSchema(),
			Model:        modelIdentifier(),
		}

		events, err := client.Stream(ctx, req)
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return "canceled", ""
			}
			return fmt.Sprintf("error generating agent: %v", err), ""
		}

		aggregator := newJSONChunkAggregator()
		var textBuilder strings.Builder

		for event := range events {
			chunk := event.Data
			if chunk.JSONPath != "" || chunk.ChunkType == "json" {
				path := chunk.JSONPath
				if path == "" {
					path = "text"
				}
				aggregator.Add(path, chunk.Delta)
				if path == "text" {
					if deltaStr, ok := chunk.Delta.(string); ok {
						textBuilder.WriteString(deltaStr)
					}
				}
				continue
			}
			if deltaStr, ok := chunk.Delta.(string); ok && deltaStr != "" {
				textBuilder.WriteString(deltaStr)
			}
		}

		assembled, err := aggregator.Assemble()
		if err != nil {
			return fmt.Sprintf("error assembling response: %v", err), ""
		}

		var output sessionOutput
		if assembled != "" {
			if err := json.Unmarshal([]byte(assembled), &output); err != nil {
				var wrapper struct {
					Result sessionOutput `json:"result"`
				}
				if err := json.Unmarshal([]byte(assembled), &wrapper); err != nil {
					return fmt.Sprintf("error decoding response: %v", err), ""
				}
				output = wrapper.Result
			}
		}

		text := strings.TrimSpace(output.Text)
		if text == "" {
			text = strings.TrimSpace(textBuilder.String())
		}

		if len(output.Tools) == 0 {
			metaObj := map[string]any{
				"task_definition":  strings.TrimSpace(taskDefinition),
				"agent_name":       strings.TrimSpace(agentDisplay),
				"agent_identifier": strings.TrimSpace(resolvedAgentID),
				"transcript":       transcript,
			}
			if trimmedParam := strings.TrimSpace(agentParameter); trimmedParam != "" {
				metaObj["agent_parameter"] = trimmedParam
			}
			if mb, err := json.Marshal(metaObj); err == nil {
				metadata = string(mb)
			}
			return text, metadata
		}

		for _, tool := range output.Tools {
			name := strings.TrimSpace(tool.Name)
			if name == "" {
				continue
			}
			argsJSON := marshalArgs(tool.Arguments)
			callSeq++
			callID := generateToolCallID()
			uid := fmt.Sprintf("%d", callSeq)

			start := SubAgentEvent{
				Kind:           "tool",
				Status:         "start",
				ToolCallID:     callID,
				CallUID:        uid,
				ToolName:       name,
				ToolInput:      argsJSON,
				TaskDefinition: taskDefinition,
				AgentName:      agentDisplay,
			}
			transcript = append(transcript, start)
			if progress != nil {
				progress(start)
			}

			if ctx != nil {
				select {
				case <-ctx.Done():
					return "canceled", ""
				default:
				}
			}

			allowed, denyMessage := requestToolPermission(perms, workingDir, "", callID, name, argsJSON, tool.Reason)

			var (
				result string
				meta   string
			)

			if !allowed {
				result = permission.ErrorPermissionDenied.Error()
				if trimmed := strings.TrimSpace(denyMessage); trimmed != "" {
					result = trimmed
				}
			} else {
				result, meta = runner.Execute(ctx, name, argsJSON, func(ev SubAgentEvent) {
					ev.ToolCallID = callID
					ev.AgentName = agentDisplay
					ev.TaskDefinition = taskDefinition
					if progress != nil {
						progress(ev)
					}
				})
			}

			finish := SubAgentEvent{
				Kind:               "tool",
				Status:             "finish",
				ToolCallID:         callID,
				CallUID:            uid,
				ToolName:           name,
				ToolInput:          argsJSON,
				ToolResultContent:  result,
				ToolResultMetadata: meta,
				TaskDefinition:     taskDefinition,
				AgentName:          agentDisplay,
			}
			transcript = append(transcript, finish)
			if progress != nil {
				progress(finish)
			}

			conversation = append(conversation, map[string]any{
				"role": "assistant",
				"tool_calls": []map[string]any{
					{
						"id":   callID,
						"type": "function",
						"function": map[string]any{
							"name":      name,
							"arguments": argsJSON,
						},
					},
				},
			})
			toolMsg := map[string]any{
				"role":    "tool_call_output",
				"tool_id": callID,
				"content": result,
			}
			if trimmedMeta := strings.TrimSpace(meta); trimmedMeta != "" {
				toolMsg["metadata"] = trimmedMeta
			}
			conversation = append(conversation, toolMsg)
		}
	}

	return "error: too many tool passes", ""
}

func builderAgentInstructions() string {
	return "You are a focused helper agent. Use the provided tools when necessary and respond succinctly."
}

func remoteAgentInstructions(systemPrompt string, replace bool, agentName string) string {
	trimmed := strings.TrimSpace(systemPrompt)
	if trimmed == "" {
		return fmt.Sprintf("You are the managed agent \"%s\". Use the provided command tools to fulfill the user's request and respond succinctly.", strings.TrimSpace(agentName))
	}
	if replace {
		return trimmed
	}
	var b strings.Builder
	b.WriteString(trimmed)
	b.WriteString("\n\nUse the provided command tools to fulfill the user's request and respond succinctly.")
	return b.String()
}

func ModelName() string { return "gcp/gemini-flash-latest" }
