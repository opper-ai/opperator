package tui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/google/uuid"

	"tui/coreagent"
	"tui/internal/protocol"
	"tui/secretprompt"
	sessionstate "tui/sessionstate"
	streaming "tui/streaming"
	tooling "tui/tools"
	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
	toolstate "tui/toolstate"
	"tui/util"
)

// colorToLipgloss converts a color.Color to a lipgloss color
func colorToLipgloss(c color.Color) color.Color {
	if rgba, ok := c.(color.RGBA); ok {
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", rgba.R, rgba.G, rgba.B))
	}
	r, g, b, _ := c.RGBA()
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8)))
}

// agentColorOrSecondary returns the agent color or a fallback color
func agentColorOrSecondary(raw string, fallback color.Color) color.Color {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return colorToLipgloss(fallback)
	}
	return lipgloss.Color(trimmed)
}

// batchCmds batches multiple commands into a single command
func batchCmds(cmds []tea.Cmd) tea.Cmd {
	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	default:
		return tea.Batch(cmds...)
	}
}

// keyString extracts the key string from a message
func keyString(msg tea.Msg) (string, bool) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		return v.String(), true
	case tea.KeyPressMsg:
		return v.String(), true
	default:
		return "", false
	}
}

// agentStatusMessage returns the status message and hint for an agent
func agentStatusMessage(status string) (string, string) {
	value := strings.ToLower(strings.TrimSpace(status))
	switch value {
	case "", "running":
		return "running", ""
	case "stopped":
		return "stopped", "start before selecting"
	case "crashed":
		return "crashed", "inform user and ask to debug"
	default:
		return value, ""
	}
}

// agentOptionDescriptor returns the descriptor for an agent option
func agentOptionDescriptor(description, status string) string {
	name := strings.TrimSpace(description)
	if name == "" {
		name = "(no description)"
	}
	state, hint := agentStatusMessage(status)
	if state == "" {
		return fmt.Sprintf("%s", name)
	}
	if hint != "" {
		return fmt.Sprintf("%s\nStatus: %s (%s)", name, state, hint)
	}
	return fmt.Sprintf("%s\nStatus: %s", name, state)
}

// agentDescriptorBlock returns a formatted agent descriptor block
func agentDescriptorBlock(name, description, status string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	desc := agentOptionDescriptor(description, status)
	return fmt.Sprintf("%s\n%s", trimmed, desc)
}

// agentListInstructions returns formatted instructions for available agents
func agentListInstructions(options []sessionAgentOption, listErr error) string {
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

// hasSlashExposure checks if a command has slash command exposure
func hasSlashExposure(cmd protocol.CommandDescriptor) bool {
	for _, exposure := range cmd.ExposeAs {
		if exposure == protocol.CommandExposureSlashCommand {
			return true
		}
	}
	return false
}

// countCommandExposures counts the number of tool and slash command exposures
func countCommandExposures(cmds []protocol.CommandDescriptor) (toolCount int, slashCount int) {
	for _, cmd := range cmds {
		seenTool := false
		seenSlash := false
		if len(cmd.ExposeAs) == 0 {
			toolCount++
			continue
		}
		for _, exposure := range cmd.ExposeAs {
			switch exposure {
			case protocol.CommandExposureAgentTool:
				if !seenTool {
					toolCount++
					seenTool = true
				}
			case protocol.CommandExposureSlashCommand:
				if !seenSlash {
					slashCount++
					seenSlash = true
				}
			}
		}
	}
	return
}

// progressEntriesFromAsync converts async task progress to progress records
func progressEntriesFromAsync(entries []tooling.AsyncTaskProgress) []toolstate.ProgressRecord {
	if len(entries) == 0 {
		return nil
	}
	out := make([]toolstate.ProgressRecord, 0, len(entries))
	for _, entry := range entries {
		text := strings.TrimSpace(entry.Text)
		status := strings.TrimSpace(entry.Status)
		metadata := strings.TrimSpace(entry.Metadata)
		if text == "" && status == "" && metadata == "" && entry.Timestamp.IsZero() {
			continue
		}
		out = append(out, toolstate.ProgressRecord{
			Timestamp: entry.Timestamp,
			Text:      text,
			Status:    status,
			Metadata:  metadata,
		})
	}
	return out
}

// lifecycleFromAsyncStatus converts async task status to lifecycle state
func lifecycleFromAsyncStatus(status string, isError bool) toolstate.Lifecycle {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pending":
		return toolstate.LifecyclePending
	case "loading", "running", "in_progress", "active":
		return toolstate.LifecycleRunning
	case "complete", "completed", "success":
		if isError {
			return toolstate.LifecycleFailed
		}
		return toolstate.LifecycleCompleted
	case "failed", "error", "canceled", "cancelled":
		return toolstate.LifecycleFailed
	case "deleted":
		return toolstate.LifecycleDeleted
	default:
		if isError {
			return toolstate.LifecycleFailed
		}
		return toolstate.LifecycleUnknown
	}
}

// asyncFlagsForTask returns execution flags for an async task
func asyncFlagsForTask(task tooling.AsyncTask) toolstate.ExecutionFlags {
	flags := toolstate.ExecutionFlags{Async: true}
	switch strings.ToLower(strings.TrimSpace(task.Mode)) {
	case "persistent", "watch", "continuous", "daemon":
		flags.Persistent = true
	}
	return flags
}

// asyncTaskCallName returns the call name for an async task
func asyncTaskCallName(task tooling.AsyncTask) string {
	if trimmed := strings.TrimSpace(task.ToolName); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(task.CommandName); trimmed != "" {
		return trimmed
	}
	return tooling.AsyncToolName
}

// asyncTaskDisplayLabel returns the display label for an async task
func asyncTaskDisplayLabel(task tooling.AsyncTask) string {
	meta := tooling.ParseTaskMetadata(task.Metadata)
	if trimmed := strings.TrimSpace(meta.Label); trimmed != "" {
		return trimmed
	}
	if target, ok := tooling.LookupAgentCommandTool(task.ToolName); ok {
		if trimmed := strings.TrimSpace(target.Label); trimmed != "" {
			return trimmed
		}
	}
	if trimmed := strings.TrimSpace(task.CommandName); trimmed != "" {
		label := toolregistry.PrettifyName(trimmed)
		return label
	}
	if trimmed := strings.TrimSpace(task.ToolName); trimmed != "" {
		if def, ok := toolregistry.Lookup(trimmed); ok {
			if label := strings.TrimSpace(def.Label); label != "" {
				return label
			}
		}
		label := toolregistry.PrettifyName(trimmed)
		return label
	}
	return ""
}

// buildProgressRecords builds progress records from async task progress
func buildProgressRecords(entries []tooling.AsyncTaskProgress) []any {
	if len(entries) == 0 {
		return nil
	}
	records := make([]any, 0, len(entries))
	for _, entry := range entries {
		if record := progressRecord(entry); record != nil {
			records = append(records, record)
		}
	}
	if len(records) == 0 {
		return nil
	}
	return records
}

// buildAsyncTaskMetadata builds metadata for an async task
func buildAsyncTaskMetadata(task tooling.AsyncTask) string {
	trimmed := strings.TrimSpace(task.Metadata)
	meta := map[string]any{}
	if trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &meta); err != nil || meta == nil {
			meta = map[string]any{"original_metadata": trimmed}
		}
	}
	if meta == nil {
		meta = make(map[string]any)
	}
	if len(task.Progress) > 0 {
		meta["progress"] = buildProgressRecords(task.Progress)
	}
	asyncInfo := map[string]any{}
	if trimmedID := strings.TrimSpace(task.ID); trimmedID != "" {
		asyncInfo["id"] = trimmedID
	}
	if trimmedStatus := strings.TrimSpace(task.Status); trimmedStatus != "" {
		asyncInfo["status"] = trimmedStatus
	}
	if trimmedSession := strings.TrimSpace(task.SessionID); trimmedSession != "" {
		asyncInfo["session_id"] = trimmedSession
	}
	if trimmedCall := strings.TrimSpace(task.CallID); trimmedCall != "" {
		asyncInfo["call_id"] = trimmedCall
	}
	if task.CompletedAt != nil && !task.CompletedAt.IsZero() {
		asyncInfo["completed_at"] = task.CompletedAt.Format(time.RFC3339)
	}
	if len(asyncInfo) > 0 {
		meta["async_task"] = asyncInfo
	}
	if trimmedTool := strings.TrimSpace(task.ToolName); trimmedTool != "" {
		meta["tool_name"] = trimmedTool
	}
	if trimmedArgs := strings.TrimSpace(task.Args); trimmedArgs != "" {
		meta["async_task_args"] = trimmedArgs
	}
	bytes, err := json.Marshal(meta)
	if err != nil {
		return trimmed
	}
	return string(bytes)
}

// appendProgressRecords appends progress records to existing records
func appendProgressRecords(existing any, entries []tooling.AsyncTaskProgress) []any {
	var progress []any
	if current, ok := existing.([]any); ok {
		progress = append(progress, current...)
	}
	for _, entry := range entries {
		if record := progressRecord(entry); record != nil {
			progress = append(progress, record)
		}
	}
	return progress
}

// progressRecord creates a progress record from an async task progress entry
func progressRecord(entry tooling.AsyncTaskProgress) map[string]any {
	record := map[string]any{}
	if !entry.Timestamp.IsZero() {
		record["timestamp"] = entry.Timestamp.Format(time.RFC3339)
	}
	if text := strings.TrimSpace(entry.Text); text != "" {
		record["text"] = text
	}
	if status := strings.TrimSpace(entry.Status); status != "" {
		record["status"] = status
	}
	if metadata := strings.TrimSpace(entry.Metadata); metadata != "" {
		record["metadata"] = metadata
	}
	if len(record) == 0 {
		return nil
	}
	return record
}

// canonicalAsyncToolCall returns the canonical form of an async tool call
func canonicalAsyncToolCall(call tooltypes.Call) tooltypes.Call {
	if target, ok := tooling.LookupAgentCommandTool(call.Name); ok && target.Async {
		call.Name = tooling.AsyncToolName
		if trimmed := strings.TrimSpace(call.Input); trimmed != "" {
			call.Input = trimmed
		} else if trimmed := strings.TrimSpace(target.ProgressLabel); trimmed != "" {
			call.Input = trimmed
		} else {
			call.Input = "async task pending"
		}
	}
	return call
}

// parseTimeOrZero parses a time string or returns zero time
func parseTimeOrZero(value string) time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
		return ts
	}
	return time.Time{}
}

// generateAsyncCallID generates a unique call ID for an async task
func generateAsyncCallID() string {
	return "async_" + uuid.NewString()
}

// extractAsyncTaskMetadata extracts async task metadata from a raw string
func extractAsyncTaskMetadata(raw string) (taskID, sessionID, callID, tool string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", "", ""
	}
	var wrapper struct {
		AsyncTask struct {
			ID        string `json:"id"`
			SessionID string `json:"session_id"`
			CallID    string `json:"call_id"`
			Tool      string `json:"tool"`
		} `json:"async_task"`
		Task struct {
			ID        string `json:"id"`
			SessionID string `json:"session_id"`
			CallID    string `json:"call_id"`
			Tool      string `json:"tool"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err != nil {
		return "", "", "", ""
	}
	info := wrapper.AsyncTask
	if strings.TrimSpace(info.ID) == "" {
		info = wrapper.Task
	}
	return strings.TrimSpace(info.ID), strings.TrimSpace(info.SessionID), strings.TrimSpace(info.CallID), strings.TrimSpace(info.Tool)
}

// firstNonEmpty returns the first non-empty string from the values
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// asyncResultAppearsComplete checks if an async result appears to be complete
func asyncResultAppearsComplete(result tooltypes.Result) bool {
	if result.IsError {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(result.Content))
	if status == "" {
		return false
	}
	if strings.Contains(status, "pending") || strings.Contains(status, "scheduled") || strings.Contains(status, "running") {
		return false
	}
	return true
}

// readLocalFileHead reads the first maxLines lines from a file
func readLocalFileHead(path string, maxLines int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("error reading %s: %v", path, err)
	}
	lines := strings.Split(string(data), "\n")
	if maxLines <= 0 || maxLines > len(lines) {
		maxLines = len(lines)
	}
	return strings.Join(lines[:maxLines], "\n")
}

// Model accessor methods

// permissionUI returns the permission UI controller
func (m *Model) permissionUI() *permissionUI {
	return m.PermissionController.ui
}

// secretPromptUI returns the secret prompt UI controller
func (m *Model) secretPromptUI() *secretPromptUI {
	return m.SecretPromptController.ui
}

// secretPromptService returns the secret prompt service
func (m *Model) secretPromptService() secretprompt.Service {
	return m.SecretPromptController.prompts
}

// sessionManager returns the session manager, creating it if needed
func (m *Model) sessionManager() *sessionstate.Manager {
	if m.historyMgr == nil {
		m.historyMgr = sessionstate.NewManager(m.convStore, m.msgStore, m.inputStore)
	}
	return m.historyMgr
}

// streamManager returns the stream manager, creating it if needed
func (m *Model) streamManager() *streaming.Manager {
	if m.StreamTracker.manager == nil {
		m.StreamTracker.manager = streaming.NewManager()
	}
	return m.StreamTracker.manager
}

// ReportInfo reports an info message
func (m *Model) ReportInfo(info string) tea.Cmd {
	return util.ReportInfo(info)
}

// ReportWarn reports a warning message
func (m *Model) ReportWarn(warn string) tea.Cmd {
	return util.ReportWarn(warn)
}

// ReportError reports an error
func (m *Model) ReportError(err error) tea.Cmd {
	return util.ReportError(err)
}
