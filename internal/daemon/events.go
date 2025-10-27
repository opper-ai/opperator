package daemon

import (
	"opperator/internal/protocol"
	"opperator/internal/taskqueue"
	"tui/components/sidebar"
)

// AgentStateChangeType identifies what kind of state change occurred
type AgentStateChangeType string

const (
	AgentStateMetadata AgentStateChangeType = "metadata" // Description, system prompt, color changed
	AgentStateLogs     AgentStateChangeType = "logs"     // Logs updated
	AgentStateSections AgentStateChangeType = "sections" // Custom sections changed
	AgentStateStatus   AgentStateChangeType = "status"   // Agent started/stopped
	AgentStateCommands AgentStateChangeType = "commands" // Command registry updated
)

type AgentStateChange struct {
	AgentName string
	Type      AgentStateChangeType

	// Metadata fields (populated when Type == AgentStateMetadata)
	Description  string
	SystemPrompt string
	Color        string

	// Logs fields (populated when Type == AgentStateLogs)
	Logs     []string // For bulk log updates (initial load)
	LogEntry string   // For single log append events

	CustomSections []sidebar.CustomSection
	Commands       []protocol.CommandDescriptor

	// Status fields (populated when Type == AgentStateStatus)
	Status string
}

// TaskEventType identifies what kind of task event occurred
type TaskEventType string

const (
	TaskEventSnapshot  TaskEventType = "snapshot"  // Snapshot of task state
	TaskEventProgress  TaskEventType = "progress"  // Task emitted progress
	TaskEventCompleted TaskEventType = "completed" // Task completed successfully
	TaskEventFailed    TaskEventType = "failed"    // Task failed
	TaskEventDeleted   TaskEventType = "deleted"   // Task was deleted
)

type TaskEvent struct {
	Type TaskEventType
	Task *taskqueue.Task
}
