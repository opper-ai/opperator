package message

import "time"

// Rich content parts (subset of the reference model).

type ContentPart interface{ isPart() }

type TextContent struct {
	Text string `json:"text"`
}

func (TextContent) isPart()           {}
func (tc TextContent) String() string { return tc.Text }

type ToolCall struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Input    string `json:"input"`
	Type     string `json:"type"`
	Finished bool   `json:"finished"`
	Reason   string `json:"reason,omitempty"`
	Async    bool   `json:"async,omitempty"`
}

func (ToolCall) isPart() {}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	Metadata   string `json:"metadata"`
	IsError    bool   `json:"is_error"`
	Pending    bool   `json:"pending"`
}

func (ToolResult) isPart() {}

type FinishReason string

const (
	FinishReasonEndTurn  FinishReason = "end_turn"
	FinishReasonToolUse  FinishReason = "tool_use"
	FinishReasonCanceled FinishReason = "canceled"
	FinishReasonError    FinishReason = "error"
)

type Finish struct {
	Reason  FinishReason `json:"reason"`
	Time    int64        `json:"time"`
	Message string       `json:"message,omitempty"`
	Details string       `json:"details,omitempty"`
}

func (Finish) isPart() {}

// TurnSummary marks the completion of an assistant turn with metadata.
type TurnSummary struct {
	AgentID       string `json:"agent_id"`
	AgentName     string `json:"agent_name,omitempty"`
	AgentColor    string `json:"agent_color,omitempty"`
	DurationMilli int64  `json:"duration_ms"`
}

func (TurnSummary) isPart() {}

func (m *Message) Content() TextContent {
	for _, p := range m.Parts {
		if t, ok := p.(TextContent); ok {
			return t
		}
	}
	return TextContent{}
}

func (m *Message) ToolCalls() []ToolCall {
	var out []ToolCall
	for _, p := range m.Parts {
		if t, ok := p.(ToolCall); ok {
			out = append(out, t)
		}
	}
	return out
}

func (m *Message) ToolResults() []ToolResult {
	var out []ToolResult
	for _, p := range m.Parts {
		if t, ok := p.(ToolResult); ok {
			out = append(out, t)
		}
	}
	return out
}

func (m *Message) AddFinish(reason FinishReason, message, details string) {
	m.Parts = append(m.Parts, Finish{Reason: reason, Time: time.Now().Unix(), Message: message, Details: details})
}
