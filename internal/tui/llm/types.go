package llm

import (
	"time"

	tooling "tui/tools"
	tooltypes "tui/tools/types"
)

// Adapter exposes the conversation operations required by the LLM engine.
type Adapter interface {
	BuildInstructions() string
	BuildConversation() []map[string]any
	SessionID() string
	BaseToolSpecs() []tooling.Spec
	ExtraToolSpecs() []tooling.Spec
	AgentToolSpec() tooling.Spec
	ParentSpanID() string
	RecordSpanID(spanID string)
	RecordAssistantContent(text string)
	RecordAssistantToolCalls(calls []tooltypes.Call, content string)
	RecordToolResults(results []tooltypes.Result)
	RecordTurnCompletion(duration time.Duration)
	LastAssistantContent() string
	ActiveAgentName() string
	CoreAgentID() string
}

// Stream messages emitted by the LLM engine and consumed by the TUI model.
type (
	StreamStartedMsg struct{ Err error }
	StreamDeltaMsg   struct{ Text string }
	StreamDoneMsg    struct{ Err error }

	ToolUseDeltaMsg struct {
		ID    string
		Name  string
		Delta string
	}

	ToolUseStartMsg  struct{ Call tooltypes.Call }
	ToolUseFinishMsg struct{ Result tooltypes.Result }

	SubAgentEventMsg struct {
		ID string
		Ev SubAgentEvent
	}

	FollowupStartMsg struct{}

	AsyncToolUpdateMsg struct {
		SessionID string
		CallID    string
		Task      *tooling.AsyncTask
		Result    *tooltypes.Result
		Error     string
		Progress  []tooling.AsyncTaskProgress
	}
)

// SubAgentEvent mirrors updates emitted by nested agents.
type SubAgentEvent struct {
	Kind               string
	Status             string
	Content            string
	ToolCallID         string
	CallUID            string
	ToolName           string
	ToolInput          string
	ToolResultContent  string
	ToolResultMetadata string
	TaskDefinition     string
	AgentName          string
}
