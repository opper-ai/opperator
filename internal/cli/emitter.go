package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// EventEmitter defines the interface for emitting execution events
type EventEmitter interface {
	// Session events
	EmitSessionStarted(event SessionStartedEvent)
	EmitSessionCompleted(event SessionCompletedEvent)
	EmitSessionFailed(event SessionFailedEvent)

	// Turn events
	EmitTurnStarted(event TurnStartedEvent)
	EmitTurnCompleted(event TurnCompletedEvent)
	EmitTurnFailed(event TurnFailedEvent)

	// Item events
	EmitItemStarted(event ItemEvent)
	EmitItemUpdated(event ItemEvent)
	EmitItemCompleted(event ItemEvent)

	// Sub-agent events
	EmitSubAgentStarted(event SubAgentStartedEvent)
	EmitSubAgentCompleted(event SubAgentCompletedEvent)
	EmitSubAgentFailed(event SubAgentFailedEvent)
	EmitSubAgentTurnStarted(event SubAgentTurnStartedEvent)
	EmitSubAgentTurnCompleted(event SubAgentTurnCompletedEvent)
	EmitSubAgentItemStarted(event SubAgentItemEvent)
	EmitSubAgentItemUpdated(event SubAgentItemEvent)
	EmitSubAgentItemCompleted(event SubAgentItemEvent)

	// Async task events
	EmitAsyncTaskScheduled(event AsyncTaskScheduledEvent)
	EmitAsyncTaskSnapshot(event AsyncTaskSnapshotEvent)
	EmitAsyncTaskProgress(event AsyncTaskProgressEvent)
	EmitAsyncTaskCompleted(event AsyncTaskCompletedEvent)
	EmitAsyncTaskFailed(event AsyncTaskFailedEvent)
	EmitAsyncTaskDeleted(event AsyncTaskDeletedEvent)

	// Command progress events
	EmitCommandProgress(event CommandProgressEvent)

	// Helper methods for pretty-printing (stderr mode)
	PrintAgentInfo(agentName, agentType, description string, toolCount int)
	PrintSeparator()
	PrintSectionHeader(text string)
	PrintToolExecution(toolName, displayName string)
	PrintToolSuccess(message string)
	PrintToolError(message string)
	PrintToolProgress(lines []string)
	PrintToolOutput(lines []string)
	PrintSubAgentHeader(agentName, taskDef string)
	PrintSubAgentComplete()
	PrintContinuing()
	PrintStreamingText(text string)
	PrintStreamingComplete()
	PrintResumeInfo(conversationID string)
}

// JSONEmitter writes events as JSON Lines to stdout
type JSONEmitter struct {
	mu     sync.Mutex
	output io.Writer
}

// NewJSONEmitter creates a new JSON emitter that writes to stdout
func NewJSONEmitter() *JSONEmitter {
	return &JSONEmitter{
		output: os.Stdout,
	}
}

func (e *JSONEmitter) emit(event interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()

	data, err := json.Marshal(event)
	if err != nil {
		// Log to stderr on serialization failure
		fmt.Fprintf(os.Stderr, "ERROR: Failed to serialize event: %v\n", err)
		return
	}

	fmt.Fprintln(e.output, string(data))

	// Flush to ensure streaming behavior
	if f, ok := e.output.(*os.File); ok {
		f.Sync()
	}
}

func (e *JSONEmitter) EmitSessionStarted(event SessionStartedEvent) {
	event.Type = EventSessionStarted
	e.emit(event)
}

func (e *JSONEmitter) EmitSessionCompleted(event SessionCompletedEvent) {
	event.Type = EventSessionCompleted
	e.emit(event)
}

func (e *JSONEmitter) EmitSessionFailed(event SessionFailedEvent) {
	event.Type = EventSessionFailed
	e.emit(event)
}

func (e *JSONEmitter) EmitTurnStarted(event TurnStartedEvent) {
	event.Type = EventTurnStarted
	e.emit(event)
}

func (e *JSONEmitter) EmitTurnCompleted(event TurnCompletedEvent) {
	event.Type = EventTurnCompleted
	e.emit(event)
}

func (e *JSONEmitter) EmitTurnFailed(event TurnFailedEvent) {
	event.Type = EventTurnFailed
	e.emit(event)
}

func (e *JSONEmitter) EmitItemStarted(event ItemEvent) {
	event.Type = EventItemStarted
	e.emit(event)
}

func (e *JSONEmitter) EmitItemUpdated(event ItemEvent) {
	event.Type = EventItemUpdated
	e.emit(event)
}

func (e *JSONEmitter) EmitItemCompleted(event ItemEvent) {
	event.Type = EventItemCompleted
	e.emit(event)
}

func (e *JSONEmitter) EmitSubAgentStarted(event SubAgentStartedEvent) {
	event.Type = EventSubAgentStarted
	e.emit(event)
}

func (e *JSONEmitter) EmitSubAgentCompleted(event SubAgentCompletedEvent) {
	event.Type = EventSubAgentCompleted
	e.emit(event)
}

func (e *JSONEmitter) EmitSubAgentFailed(event SubAgentFailedEvent) {
	event.Type = EventSubAgentFailed
	e.emit(event)
}

func (e *JSONEmitter) EmitSubAgentTurnStarted(event SubAgentTurnStartedEvent) {
	event.Type = EventSubAgentTurnStarted
	e.emit(event)
}

func (e *JSONEmitter) EmitSubAgentTurnCompleted(event SubAgentTurnCompletedEvent) {
	event.Type = EventSubAgentTurnCompleted
	e.emit(event)
}

func (e *JSONEmitter) EmitSubAgentItemStarted(event SubAgentItemEvent) {
	event.Type = EventSubAgentItemStarted
	e.emit(event)
}

func (e *JSONEmitter) EmitSubAgentItemUpdated(event SubAgentItemEvent) {
	event.Type = EventSubAgentItemUpdated
	e.emit(event)
}

func (e *JSONEmitter) EmitSubAgentItemCompleted(event SubAgentItemEvent) {
	event.Type = EventSubAgentItemCompleted
	e.emit(event)
}

func (e *JSONEmitter) EmitAsyncTaskScheduled(event AsyncTaskScheduledEvent) {
	event.Type = EventAsyncTaskScheduled
	e.emit(event)
}

func (e *JSONEmitter) EmitAsyncTaskSnapshot(event AsyncTaskSnapshotEvent) {
	event.Type = EventAsyncTaskSnapshot
	e.emit(event)
}

func (e *JSONEmitter) EmitAsyncTaskProgress(event AsyncTaskProgressEvent) {
	event.Type = EventAsyncTaskProgress
	e.emit(event)
}

func (e *JSONEmitter) EmitAsyncTaskCompleted(event AsyncTaskCompletedEvent) {
	event.Type = EventAsyncTaskCompleted
	e.emit(event)
}

func (e *JSONEmitter) EmitAsyncTaskFailed(event AsyncTaskFailedEvent) {
	event.Type = EventAsyncTaskFailed
	e.emit(event)
}

func (e *JSONEmitter) EmitAsyncTaskDeleted(event AsyncTaskDeletedEvent) {
	event.Type = EventAsyncTaskDeleted
	e.emit(event)
}

func (e *JSONEmitter) EmitCommandProgress(event CommandProgressEvent) {
	event.Type = EventCommandProgress
	e.emit(event)
}

// Helper methods (no-ops for JSON mode)
func (e *JSONEmitter) PrintAgentInfo(agentName, agentType, description string, toolCount int) {}
func (e *JSONEmitter) PrintSeparator()                                                         {}
func (e *JSONEmitter) PrintSectionHeader(text string)                                          {}
func (e *JSONEmitter) PrintToolExecution(toolName, displayName string)                         {}
func (e *JSONEmitter) PrintToolSuccess(message string)                                         {}
func (e *JSONEmitter) PrintToolError(message string)                                           {}
func (e *JSONEmitter) PrintToolProgress(lines []string)                                        {}
func (e *JSONEmitter) PrintToolOutput(lines []string)                                          {}
func (e *JSONEmitter) PrintSubAgentHeader(agentName, taskDef string)                           {}
func (e *JSONEmitter) PrintSubAgentComplete()                                                  {}
func (e *JSONEmitter) PrintContinuing()                                                        {}
func (e *JSONEmitter) PrintStreamingText(text string)                                          {}
func (e *JSONEmitter) PrintStreamingComplete()                                                 {}
func (e *JSONEmitter) PrintResumeInfo(conversationID string)                                   {}

// StderrEmitter writes pretty-formatted output to stderr (existing behavior)
type StderrEmitter struct {
	mu sync.Mutex
}

// NewStderrEmitter creates a new stderr emitter (pretty-print mode)
func NewStderrEmitter() *StderrEmitter {
	return &StderrEmitter{}
}

// Event methods (no-ops for stderr mode - we use Print* methods instead)
func (e *StderrEmitter) EmitSessionStarted(event SessionStartedEvent)                {}
func (e *StderrEmitter) EmitSessionCompleted(event SessionCompletedEvent)            {}
func (e *StderrEmitter) EmitSessionFailed(event SessionFailedEvent)                  {}
func (e *StderrEmitter) EmitTurnStarted(event TurnStartedEvent)                      {}
func (e *StderrEmitter) EmitTurnCompleted(event TurnCompletedEvent)                  {}
func (e *StderrEmitter) EmitTurnFailed(event TurnFailedEvent)                        {}
func (e *StderrEmitter) EmitItemStarted(event ItemEvent)                             {}
func (e *StderrEmitter) EmitItemUpdated(event ItemEvent)                             {}
func (e *StderrEmitter) EmitItemCompleted(event ItemEvent)                           {}
func (e *StderrEmitter) EmitSubAgentStarted(event SubAgentStartedEvent)              {}
func (e *StderrEmitter) EmitSubAgentCompleted(event SubAgentCompletedEvent)          {}
func (e *StderrEmitter) EmitSubAgentFailed(event SubAgentFailedEvent)                {}
func (e *StderrEmitter) EmitSubAgentTurnStarted(event SubAgentTurnStartedEvent)      {}
func (e *StderrEmitter) EmitSubAgentTurnCompleted(event SubAgentTurnCompletedEvent)  {}
func (e *StderrEmitter) EmitSubAgentItemStarted(event SubAgentItemEvent)             {}
func (e *StderrEmitter) EmitSubAgentItemUpdated(event SubAgentItemEvent)             {}
func (e *StderrEmitter) EmitSubAgentItemCompleted(event SubAgentItemEvent)           {}
func (e *StderrEmitter) EmitAsyncTaskScheduled(event AsyncTaskScheduledEvent)        {}
func (e *StderrEmitter) EmitAsyncTaskSnapshot(event AsyncTaskSnapshotEvent)          {}
func (e *StderrEmitter) EmitAsyncTaskProgress(event AsyncTaskProgressEvent)          {}
func (e *StderrEmitter) EmitAsyncTaskCompleted(event AsyncTaskCompletedEvent)        {}
func (e *StderrEmitter) EmitAsyncTaskFailed(event AsyncTaskFailedEvent)              {}
func (e *StderrEmitter) EmitAsyncTaskDeleted(event AsyncTaskDeletedEvent)            {}
func (e *StderrEmitter) EmitCommandProgress(event CommandProgressEvent)              {}

// Helper methods (use existing styles from exec.go)
func (e *StderrEmitter) PrintAgentInfo(agentName, agentType, description string, toolCount int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	fmt.Fprintln(os.Stderr, labelStyle.Render("Agent:")+" "+valueStyle.Render(agentName))
	if agentType == AgentTypeCore {
		fmt.Fprintln(os.Stderr, "  "+mutedStyle.Render("Core agent"))
	} else if description != "" {
		fmt.Fprintln(os.Stderr, "  "+mutedStyle.Render(description))
	}
	if toolCount > 0 {
		fmt.Fprintln(os.Stderr, "  "+mutedStyle.Render(fmt.Sprintf("Tools: %d available", toolCount)))
	}
	fmt.Fprintln(os.Stderr, mutedStyle.Render("────────────────────────────────────────────────"))
}

func (e *StderrEmitter) PrintSeparator() {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintln(os.Stderr, mutedStyle.Render("────────────────────────────────────────────────"))
}

func (e *StderrEmitter) PrintSectionHeader(text string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, sectionStyle.Render(text))
	fmt.Fprintln(os.Stderr, "")
}

func (e *StderrEmitter) PrintToolExecution(toolName, displayName string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	name := displayName
	if name == "" {
		name = toolName
	}
	fmt.Fprintln(os.Stderr, "  "+mutedStyle.Render("→")+" "+labelStyle.Render(name))
}

func (e *StderrEmitter) PrintToolSuccess(message string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintln(os.Stderr, "    "+successStyle.Render("✓")+" "+mutedStyle.Render(message))
}

func (e *StderrEmitter) PrintToolError(message string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintln(os.Stderr, "    "+errorStyle.Render("✗")+" "+mutedStyle.Render(message))
}

func (e *StderrEmitter) PrintToolProgress(lines []string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(lines) == 0 {
		return
	}

	// Clear previous lines and redraw
	fmt.Fprint(os.Stderr, "\r\033[K") // Clear current line
	for i := 0; i < len(lines)-1; i++ {
		fmt.Fprint(os.Stderr, "\033[1A\033[K") // Move up and clear
	}

	// Print all progress lines
	for _, line := range lines {
		fmt.Fprintln(os.Stderr, "    "+mutedStyle.Render(line))
	}
}

func (e *StderrEmitter) PrintToolOutput(lines []string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Just print output lines without clearing (this is final output)
	for _, line := range lines {
		if strings.TrimSpace(line) != "" && line != "\"\"" && line != "{}" && line != "null" {
			fmt.Fprintln(os.Stderr, "    "+mutedStyle.Render(line))
		}
	}
}

func (e *StderrEmitter) PrintSubAgentHeader(agentName, taskDef string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	taskDisplay := taskDef
	if taskDisplay == "" {
		taskDisplay = "Task"
	}
	fmt.Fprintln(os.Stderr, "\n"+bracketStyle.Render("[")+labelStyle.Render("Sub-Agent")+bracketStyle.Render("]")+" "+valueStyle.Render(agentName))
	fmt.Fprintln(os.Stderr, "  "+mutedStyle.Render(taskDisplay)+"\n")
}

func (e *StderrEmitter) PrintSubAgentComplete() {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, bracketStyle.Render("[")+mutedStyle.Render("Sub-Agent Complete")+bracketStyle.Render("]"))
	fmt.Fprintln(os.Stderr, "")
}

func (e *StderrEmitter) PrintContinuing() {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, veryMutedBracket.Render("[")+veryMutedStyle.Render("Continuing...")+veryMutedBracket.Render("]"))
	fmt.Fprintln(os.Stderr, "")
}

func (e *StderrEmitter) PrintStreamingText(text string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprint(os.Stderr, text)
}

func (e *StderrEmitter) PrintStreamingComplete() {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "")
}

func (e *StderrEmitter) PrintResumeInfo(conversationID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, mutedStyle.Render("────────────────────────────────────────────────"))
	fmt.Fprintln(os.Stderr, mutedStyle.Render("Continue: ")+"op exec --resume "+valueStyle.Render(conversationID))
	fmt.Fprintln(os.Stderr, "")
}
