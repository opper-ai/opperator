package sessionstate

import (
	"context"
	"strings"
	"time"

	"tui/coreagent"
	"tui/internal/protocol"
	tooling "tui/tools"
	tooltypes "tui/tools/types"
)

// AdapterOptions configures the session adapter presented to the LLM engine.
type AdapterOptions struct {
	AgentName          string
	AgentColor         string
	AgentPrompt        string
	AgentPromptReplace bool
	AgentCommands      []protocol.CommandDescriptor
	CorePrompt         string
	CoreAgentID        string
	CoreAgentName      string
	CoreAgentColor     string
	BaseSpecs          []tooling.Spec
	AgentOptions       []AgentOption
	AgentListErr       error
	FocusedAgentInfo   FocusedAgentInfo
	ExtraToolSpecs     func() []tooling.Spec
}

// Adapter bridges session state with the engine request lifecycle.
type Adapter struct {
	manager   *Manager
	sessionID string
	opts      AdapterOptions
}

// NewAdapter constructs an adapter for the given session.
func NewAdapter(manager *Manager, sessionID string, opts AdapterOptions) *Adapter {
	return &Adapter{manager: manager, sessionID: sessionID, opts: opts}
}

func (a *Adapter) BuildInstructions() string {
	// Get focused agent tools if Builder is active
	var focusedAgentTools []tooling.Spec
	if a.opts.ExtraToolSpecs != nil {
		focusedAgentTools = a.opts.ExtraToolSpecs()
	}
	return BuildInstructions(a.opts.CorePrompt, a.opts.AgentName, a.opts.AgentPrompt, a.opts.AgentPromptReplace, a.opts.AgentOptions, a.opts.AgentListErr, focusedAgentTools, a.opts.FocusedAgentInfo, a.opts.CoreAgentID)
}

// BuildConversation converts persisted history into the engine format.
func (a *Adapter) BuildConversation() []map[string]any {
	history := a.manager.ConversationHistory(context.Background(), a.sessionID)
	return BuildConversation(history)
}

func (a *Adapter) SessionID() string {
	return a.sessionID
}

// BaseToolSpecs exposes the base tool specs for the request.
func (a *Adapter) BaseToolSpecs() []tooling.Spec {
	if len(a.opts.BaseSpecs) == 0 {
		return nil
	}
	return append([]tooling.Spec(nil), a.opts.BaseSpecs...)
}

// ExtraToolSpecs enumerates agent-specific tools for the request.
func (a *Adapter) ExtraToolSpecs() []tooling.Spec {
	if a.opts.ExtraToolSpecs == nil {
		return nil
	}
	return a.opts.ExtraToolSpecs()
}

// AgentToolSpec constructs the agent picker tool specification.
func (a *Adapter) AgentToolSpec() tooling.Spec {
	currentAgent := strings.ToLower(strings.TrimSpace(a.opts.AgentName))
	coreAgentID := strings.ToLower(strings.TrimSpace(a.opts.CoreAgentID))

	// Builder should not have access to the agent tool - it uses focus_agent instead
	isBuilderActive := currentAgent == "" && coreAgentID == strings.ToLower(coreagent.IDBuilder)
	if isBuilderActive {
		return tooling.Spec{} // Return empty spec to exclude the tool
	}

	opts := make([]tooling.AgentOption, 0, len(a.opts.AgentOptions))

	for _, opt := range a.opts.AgentOptions {
		value := strings.TrimSpace(opt.Name)
		if value == "" {
			continue
		}
		// Skip if this is the current agent
		if strings.EqualFold(value, currentAgent) {
			continue
		}
		label := value
		opts = append(opts, tooling.AgentOption{
			Value:       value,
			Label:       label,
			Description: agentOptionDescriptor(opt.Description, opt.Status),
		})
	}
	return tooling.AgentSpec(opts)
}

func (a *Adapter) ParentSpanID() string {
	return a.manager.ParentSpanID(a.sessionID)
}

// RecordSpanID persists span identifiers for the session.
func (a *Adapter) RecordSpanID(spanID string) {
	a.manager.RecordSpanID(a.sessionID, spanID)
}

func (a *Adapter) RecordAssistantContent(text string) {
	a.manager.AppendAssistantContent(context.Background(), a.sessionID, text)
}

func (a *Adapter) RecordAssistantToolCalls(calls []tooltypes.Call, content string) {
	a.manager.AppendAssistantToolCalls(context.Background(), a.sessionID, calls, content)
}

// RecordToolResults persists tool results.
func (a *Adapter) RecordToolResults(results []tooltypes.Result) {
	a.manager.AppendToolResults(context.Background(), a.sessionID, results)
}

// LastAssistantContent fetches the last assistant message content.
func (a *Adapter) LastAssistantContent() string {
	return a.manager.LastAssistantContent(context.Background(), a.sessionID)
}

func (a *Adapter) RecordTurnCompletion(duration time.Duration) {
	if a.manager == nil {
		return
	}
	id, name, color := a.turnAgentDescriptor()
	ms := duration.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	a.manager.AppendTurnSummary(context.Background(), a.sessionID, TurnSummary{
		AgentID:       id,
		AgentName:     name,
		AgentColor:    color,
		DurationMilli: ms,
	})
}

func (a *Adapter) turnAgentDescriptor() (string, string, string) {
	if trimmed := strings.TrimSpace(a.opts.AgentName); trimmed != "" {
		id := trimmed
		name := strings.TrimSpace(a.opts.AgentName)
		if name == "" {
			name = id
		}
		color := strings.TrimSpace(a.opts.AgentColor)
		return id, name, color
	}
	id := strings.TrimSpace(a.opts.CoreAgentID)
	if id == "" {
		id = strings.TrimSpace(coreagent.Default().ID)
	}
	name := strings.TrimSpace(a.opts.CoreAgentName)
	if name == "" {
		name = coreagent.Default().Name
	}
	color := strings.TrimSpace(a.opts.CoreAgentColor)
	if color == "" {
		color = coreagent.Default().Color
	}
	return id, name, color
}

// ActiveAgentName returns the name of the currently active agent (if any).
func (a *Adapter) ActiveAgentName() string {
	return strings.TrimSpace(a.opts.AgentName)
}

// CoreAgentID returns the ID of the core agent.
func (a *Adapter) CoreAgentID() string {
	id := strings.TrimSpace(a.opts.CoreAgentID)
	if id == "" {
		id = strings.TrimSpace(coreagent.Default().ID)
	}
	return id
}
