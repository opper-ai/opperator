package cli

import "time"

// Event type constants
const (
	// Session events
	EventSessionStarted   = "session.started"
	EventSessionCompleted = "session.completed"
	EventSessionFailed    = "session.failed"

	// Turn events
	EventTurnStarted   = "turn.started"
	EventTurnCompleted = "turn.completed"
	EventTurnFailed    = "turn.failed"

	// Item events
	EventItemStarted   = "item.started"
	EventItemUpdated   = "item.updated"
	EventItemCompleted = "item.completed"

	// Sub-agent events
	EventSubAgentStarted       = "subagent.started"
	EventSubAgentCompleted     = "subagent.completed"
	EventSubAgentFailed        = "subagent.failed"
	EventSubAgentTurnStarted   = "subagent.turn.started"
	EventSubAgentTurnCompleted = "subagent.turn.completed"
	EventSubAgentItemStarted   = "subagent.item.started"
	EventSubAgentItemUpdated   = "subagent.item.updated"
	EventSubAgentItemCompleted = "subagent.item.completed"

	// Async task events
	EventAsyncTaskScheduled = "async_task.scheduled"
	EventAsyncTaskSnapshot  = "async_task.snapshot"
	EventAsyncTaskProgress  = "async_task.progress"
	EventAsyncTaskCompleted = "async_task.completed"
	EventAsyncTaskFailed    = "async_task.failed"
	EventAsyncTaskDeleted   = "async_task.deleted"

	// Command progress events
	EventCommandProgress = "command.progress"
)

// Item types
const (
	ItemTypeAgentMessage = "agent_message"
	ItemTypeToolCall     = "tool_call"
	ItemTypeSubAgent     = "sub_agent"
)

// Agent types
const (
	AgentTypeCore    = "core"
	AgentTypeManaged = "managed"
)

// SessionStartedEvent emitted when a session starts
type SessionStartedEvent struct {
	Type              string `json:"type"`
	SessionID         string `json:"session_id"`
	ConversationTitle string `json:"conversation_title"`
	AgentName         string `json:"agent_name"`
	AgentType         string `json:"agent_type"`
	IsResumed         bool   `json:"is_resumed"`
	HasHistory        bool   `json:"has_history"`
	MessageCount      int    `json:"message_count,omitempty"`
}

// SessionCompletedEvent emitted when a session completes successfully
type SessionCompletedEvent struct {
	Type           string `json:"type"`
	SessionID      string `json:"session_id"`
	FinalResponse  string `json:"final_response"`
	TotalTurns     int    `json:"total_turns"`
	TotalToolCalls int    `json:"total_tool_calls"`
	DurationMS     int64  `json:"duration_ms"`
}

// SessionFailedEvent emitted when a session fails
type SessionFailedEvent struct {
	Type       string `json:"type"`
	SessionID  string `json:"session_id"`
	Error      string `json:"error"`
	ErrorType  string `json:"error_type,omitempty"`
	TurnNumber int    `json:"turn_number,omitempty"`
}

// TurnStartedEvent emitted when a turn starts
type TurnStartedEvent struct {
	Type       string `json:"type"`
	SessionID  string `json:"session_id"`
	TurnNumber int    `json:"turn_number"`
	RoundCount int    `json:"round_count"`
}

// TurnCompletedEvent emitted when a turn completes
type TurnCompletedEvent struct {
	Type         string `json:"type"`
	SessionID    string `json:"session_id"`
	TurnNumber   int    `json:"turn_number"`
	RoundCount   int    `json:"round_count"`
	HasToolCalls bool   `json:"has_tool_calls"`
	DurationMS   int64  `json:"duration_ms"`
}

// TurnFailedEvent emitted when a turn fails
type TurnFailedEvent struct {
	Type       string `json:"type"`
	SessionID  string `json:"session_id"`
	TurnNumber int    `json:"turn_number"`
	Error      string `json:"error"`
}

// Item represents a discrete piece of work (agent message, tool call, sub-agent)
type Item struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Status      string         `json:"status"`
	Text        string         `json:"text,omitempty"`
	Name        string         `json:"name,omitempty"`
	DisplayName string         `json:"display_name,omitempty"`
	Arguments   map[string]any `json:"arguments,omitempty"`
	Output      string         `json:"output,omitempty"`
	Error       string         `json:"error,omitempty"`
	DurationMS  int64          `json:"duration_ms,omitempty"`
	ExitCode    *int           `json:"exit_code,omitempty"`
	AgentName   string         `json:"agent_name,omitempty"`
	Prompt      string         `json:"prompt,omitempty"`
	Result      string         `json:"result,omitempty"`
	Depth       int            `json:"depth,omitempty"`
}

// ItemEvent emitted for item lifecycle
type ItemEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Item      Item   `json:"item"`
}

// SubAgentStartedEvent emitted when a sub-agent starts
type SubAgentStartedEvent struct {
	Type           string `json:"type"`
	SessionID      string `json:"session_id"`
	SubAgentID     string `json:"subagent_id"`
	ParentItemID   string `json:"parent_item_id"`
	AgentName      string `json:"agent_name"`
	TaskDefinition string `json:"task_definition"`
	Prompt         string `json:"prompt"`
}

// TranscriptEvent represents a sub-agent transcript entry
type TranscriptEvent struct {
	Kind               string `json:"kind"`
	Status             string `json:"status"`
	ToolCallID         string `json:"tool_call_id,omitempty"`
	ToolName           string `json:"tool_name,omitempty"`
	ToolInput          string `json:"tool_input,omitempty"`
	ToolResultContent  string `json:"tool_result_content,omitempty"`
	ToolResultMetadata string `json:"tool_result_metadata,omitempty"`
}

// SubAgentMetadata contains metadata about sub-agent execution
type SubAgentMetadata struct {
	TaskDefinition   string `json:"task_definition"`
	AgentName        string `json:"agent_name"`
	TotalTurns       int    `json:"total_turns"`
	TotalToolCalls   int    `json:"total_tool_calls"`
}

// SubAgentCompletedEvent emitted when a sub-agent completes
type SubAgentCompletedEvent struct {
	Type         string             `json:"type"`
	SessionID    string             `json:"session_id"`
	SubAgentID   string             `json:"subagent_id"`
	ParentItemID string             `json:"parent_item_id"`
	Result       string             `json:"result"`
	Transcript   []TranscriptEvent  `json:"transcript,omitempty"`
	Metadata     SubAgentMetadata   `json:"metadata"`
	DurationMS   int64              `json:"duration_ms"`
}

// SubAgentFailedEvent emitted when a sub-agent fails
type SubAgentFailedEvent struct {
	Type         string `json:"type"`
	SessionID    string `json:"session_id"`
	SubAgentID   string `json:"subagent_id"`
	ParentItemID string `json:"parent_item_id"`
	Error        string `json:"error"`
}

// SubAgentTurnStartedEvent emitted when a sub-agent turn starts
type SubAgentTurnStartedEvent struct {
	Type       string `json:"type"`
	SessionID  string `json:"session_id"`
	SubAgentID string `json:"subagent_id"`
	TurnNumber int    `json:"turn_number"`
	RoundCount int    `json:"round_count"`
}

// SubAgentTurnCompletedEvent emitted when a sub-agent turn completes
type SubAgentTurnCompletedEvent struct {
	Type         string `json:"type"`
	SessionID    string `json:"session_id"`
	SubAgentID   string `json:"subagent_id"`
	TurnNumber   int    `json:"turn_number"`
	HasToolCalls bool   `json:"has_tool_calls"`
	DurationMS   int64  `json:"duration_ms"`
}

// SubAgentItemEvent emitted for sub-agent item lifecycle
type SubAgentItemEvent struct {
	Type       string `json:"type"`
	SessionID  string `json:"session_id"`
	SubAgentID string `json:"subagent_id"`
	Item       Item   `json:"item"`
}

// AsyncTaskContext contains user-friendly labels for async tasks
type AsyncTaskContext struct {
	Label    string `json:"label,omitempty"`
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
}

// AsyncTaskScheduledEvent emitted when an async task is scheduled
type AsyncTaskScheduledEvent struct {
	Type        string            `json:"type"`
	SessionID   string            `json:"session_id"`
	TaskID      string            `json:"task_id"`
	ParentItemID string           `json:"parent_item_id"`
	CallID      string            `json:"call_id"`
	ToolName    string            `json:"tool_name"`
	AgentName   string            `json:"agent_name,omitempty"`
	CommandName string            `json:"command_name,omitempty"`
	CommandArgs string            `json:"command_args,omitempty"`
	Status      string            `json:"status"`
	Daemon      string            `json:"daemon"`
	WorkingDir  string            `json:"working_dir"`
	Context     *AsyncTaskContext `json:"context,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// AsyncTaskSnapshotEvent emitted for async task current state
type AsyncTaskSnapshotEvent struct {
	Type          string    `json:"type"`
	SessionID     string    `json:"session_id"`
	TaskID        string    `json:"task_id"`
	Status        string    `json:"status"`
	ProgressCount int       `json:"progress_count"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// AsyncTaskProgressData represents a single progress update
type AsyncTaskProgressData struct {
	Timestamp time.Time `json:"timestamp"`
	Text      string    `json:"text"`
	Metadata  string    `json:"metadata,omitempty"`
	Status    string    `json:"status,omitempty"`
}

// AsyncTaskProgressEvent emitted for async task progress updates
type AsyncTaskProgressEvent struct {
	Type      string                `json:"type"`
	SessionID string                `json:"session_id"`
	TaskID    string                `json:"task_id"`
	Progress  AsyncTaskProgressData `json:"progress"`
}

// AsyncTaskCompletedEvent emitted when an async task completes
type AsyncTaskCompletedEvent struct {
	Type            string                  `json:"type"`
	SessionID       string                  `json:"session_id"`
	TaskID          string                  `json:"task_id"`
	ParentItemID    string                  `json:"parent_item_id"`
	CallID          string                  `json:"call_id"`
	Status          string                  `json:"status"`
	Result          string                  `json:"result"`
	Metadata        string                  `json:"metadata,omitempty"`
	ProgressSummary []AsyncTaskProgressData `json:"progress_summary,omitempty"`
	CompletedAt     time.Time               `json:"completed_at"`
	DurationMS      int64                   `json:"duration_ms"`
}

// AsyncTaskFailedEvent emitted when an async task fails
type AsyncTaskFailedEvent struct {
	Type            string                  `json:"type"`
	SessionID       string                  `json:"session_id"`
	TaskID          string                  `json:"task_id"`
	ParentItemID    string                  `json:"parent_item_id"`
	CallID          string                  `json:"call_id"`
	Status          string                  `json:"status"`
	Error           string                  `json:"error"`
	Result          string                  `json:"result,omitempty"`
	ProgressSummary []AsyncTaskProgressData `json:"progress_summary,omitempty"`
	CompletedAt     time.Time               `json:"completed_at"`
}

// AsyncTaskDeletedEvent emitted when an async task is deleted
type AsyncTaskDeletedEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	CallID    string `json:"call_id"`
	Error     string `json:"error,omitempty"`
}

// CommandProgressData represents command progress information
type CommandProgressData struct {
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Status   string         `json:"status,omitempty"`
	Progress float64        `json:"progress,omitempty"`
}

// CommandProgressEvent emitted for command progress updates
type CommandProgressEvent struct {
	Type      string              `json:"type"`
	SessionID string              `json:"session_id"`
	ItemID    string              `json:"item_id"`
	CommandID string              `json:"command_id"`
	Progress  CommandProgressData `json:"progress"`
}
