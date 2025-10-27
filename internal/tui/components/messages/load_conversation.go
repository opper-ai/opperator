package messages

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"tui/asyncutil"
	"tui/internal/message"
	tooling "tui/tools"
	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
	"tui/toolstate"
)

func loadConversation(c *Messages, msgs []message.Message) {
	c.initIfNeeded()
	c.items = nil
	c.focus = -1
	c.ensureVisibleIdx = -1
	c.conversationStartTime = time.Time{}
	c.cache.MarkDirtyAll()
	c.animator.Clear()
	c.lastUserIdx = -1
	c.toolStore = toolstate.NewStore()
	c.toolIndex = make(map[string]int)

	var pendingSummary *turnMeta
	lastAssistantIdx := -1

	for _, entry := range msgs {
		summary := extractTurnMeta(entry.Parts)
		switch entry.Role {
		case message.System:
			if summary != nil {
				if lastAssistantIdx >= 0 {
					if cmp, ok := c.items[lastAssistantIdx].(*messageCmp); ok {
						if cmp.applyTurnSummary(summary.agentID, summary.agentName, summary.agentColor, summary.duration) {
							c.markDirty(lastAssistantIdx)
						}
					}
					lastAssistantIdx = -1
				} else {
					pendingSummary = summary
				}
			}
			continue

		case message.User:
			cmp := newMessageCmp(message.User, entry.Content().String(), c.w, false)
			c.appendItem(cmp)
			lastAssistantIdx = -1
			continue

		case message.Assistant:
			cmp := newMessageCmp(message.Assistant, entry.Content().String(), c.w, false)
			cmp.ensureAgentDefaults(c.assistantID, c.assistantName, c.assistantColor)
			idx := c.appendItem(cmp)
			if idx >= 0 {
				lastAssistantIdx = idx
				if summary != nil {
					if cmp.applyTurnSummary(summary.agentID, summary.agentName, summary.agentColor, summary.duration) {
						c.markDirty(idx)
					}
				} else if pendingSummary != nil {
					if cmp.applyTurnSummary(pendingSummary.agentID, pendingSummary.agentName, pendingSummary.agentColor, pendingSummary.duration) {
						c.markDirty(idx)
					}
					pendingSummary = nil
				}
			}
			for _, tc := range entry.ToolCalls() {
				call := tooltypes.Call{
					ID:       tc.ID,
					Name:     tc.Name,
					Input:    tc.Input,
					Finished: tc.Finished,
					Reason:   tc.Reason,
				}
				c.EnsureToolCall(call)
				markAsyncCall(c, call, tc.Input)
			}
			continue

		case message.ToolCallRole:
			for _, tc := range entry.ToolCalls() {
				call := tooltypes.Call{
					ID:       tc.ID,
					Name:     tc.Name,
					Input:    tc.Input,
					Finished: tc.Finished,
					Reason:   tc.Reason,
				}
				c.EnsureToolCall(call)
				markAsyncCall(c, call, tc.Input)
			}
			lastAssistantIdx = -1
			continue

		case message.ToolCallResponseRole, message.Tool:
			for _, tr := range entry.ToolResults() {
				call := tooltypes.Call{
					ID:       tr.ToolCallID,
					Name:     tr.Name,
					Input:    strings.TrimSpace(tr.Content),
					Finished: inferResultCompletion(tr),
				}
				c.EnsureToolCall(call)
				markAsyncCall(c, call, tr.Name, tr.Content, tr.Metadata)
				result := tooltypes.Result{
					ToolCallID: tr.ToolCallID,
					Name:       tr.Name,
					Content:    tr.Content,
					Metadata:   tr.Metadata,
					IsError:    tr.IsError,
				}
				if call.Finished {
					c.FinishTool(tr.ToolCallID, result)
				} else {
					c.SetPendingToolResult(tr.ToolCallID, result)
				}
			}
			lastAssistantIdx = -1
			continue
		}

		lastAssistantIdx = -1
	}
}
func inferResultCompletion(tr message.ToolResult) bool {
	content := strings.TrimSpace(tr.Content)
	metadata := strings.TrimSpace(tr.Metadata)
	name := strings.ToLower(strings.TrimSpace(tr.Name))
	callID := strings.ToLower(strings.TrimSpace(tr.ToolCallID))
	isAsyncTool := name == strings.ToLower(tooling.AsyncToolName) || strings.HasPrefix(callID, "async_")

	if isAsyncTool {
		meta := toolstate.ParseMetadata(tr.Metadata)
		if status, ok := meta["async_task_status"].(string); ok {
			s := strings.ToLower(strings.TrimSpace(status))
			switch s {
			case "complete", "completed", "success", "succeeded", "done":
				return true
			case "", "pending", "running", "scheduled", "in_progress", "in-progress":
				return false
			default:
				if strings.HasPrefix(s, "pending") || strings.HasPrefix(s, "running") || strings.HasPrefix(s, "waiting") {
					return false
				}
			}
		}
		lowerContent := strings.ToLower(content)
		if lowerContent == "" {
			return false
		}
		if strings.Contains(lowerContent, "pending") || strings.Contains(lowerContent, "scheduled") || strings.Contains(lowerContent, "in progress") {
			return false
		}
		return true
	}

	if content != "" {
		return true
	}
	return metadata != ""
}

func markAsyncCall(c *Messages, call tooltypes.Call, extras ...string) {
	if c == nil {
		return
	}
	if !isAsyncContext(call.ID, call.Name, extras...) {
		return
	}
	c.SetToolFlags(call.ID, toolstate.ExecutionFlags{Async: true})
	metaLabel, _ := hintsFromAsyncMetadata(call, extras...)
	label := metaLabel
	if label == "" {
		label = asyncLabelFromSpec(call.Name)
	}
	if label == "" {
		return
	}
	if entry, ok := c.toolStore.Entry(call.ID); ok {
		if strings.TrimSpace(entry.Display.Label) != "" {
			return
		}
	}
	c.SetToolDisplay(call.ID, toolstate.ExecutionDisplay{Label: label})
}

func isAsyncContext(id, name string, extras ...string) bool {
	return asyncutil.IsContext(id, name, extras...)
}

func asyncLabelFromSpec(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || strings.EqualFold(trimmed, tooling.AsyncToolName) {
		return ""
	}
	if def, ok := toolregistry.Lookup(trimmed); ok {
		if label := strings.TrimSpace(def.Label); label != "" {
			return label
		}
	}
	return toolregistry.PrettifyName(trimmed)
}

func hintsFromAsyncMetadata(call tooltypes.Call, extras ...string) (string, string) {
	label := ""
	toolName := ""
	for _, extra := range extras {
		trimmed := strings.TrimSpace(extra)
		if trimmed == "" {
			continue
		}
		res := tooltypes.Result{
			Name:     strings.TrimSpace(call.Name),
			Metadata: trimmed,
		}
		if label == "" {
			if derived := strings.TrimSpace(tooling.PreferredAsyncLabel(call, res)); derived != "" {
				label = derived
			}
		}
		if toolName == "" {
			if candidate := extractToolNameFromMetadata(trimmed); candidate != "" {
				toolName = candidate
			}
		}
	}
	return label, toolName
}

func extractToolNameFromMetadata(raw string) string {
	var payload struct {
		Tool    string `json:"tool"`
		Command string `json:"command"`
		Task    struct {
			Tool    string `json:"tool"`
			Command string `json:"command"`
		} `json:"task"`
		AsyncTask struct {
			Tool    string `json:"tool"`
			Command string `json:"command"`
		} `json:"async_task"`
		AsyncContext struct {
			Tool    string `json:"tool"`
			Command string `json:"command"`
		} `json:"async_context"`
		CommandName string `json:"command_name"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return firstNonEmpty(
		payload.Tool,
		payload.Command,
		payload.CommandName,
		payload.AsyncTask.Tool,
		payload.AsyncTask.Command,
		payload.Task.Tool,
		payload.Task.Command,
		payload.AsyncContext.Tool,
		payload.AsyncContext.Command,
	)
}

func summarizeAsyncContext(call tooltypes.Call, extras ...string) string {
	parts := []string{fmt.Sprintf("callFinished=%v", call.Finished)}
	trimmedName := strings.TrimSpace(call.Name)
	if trimmedName != "" {
		parts = append(parts, fmt.Sprintf("name=%s", trimmedName))
	}
	for idx, extra := range extras {
		trimmed := strings.TrimSpace(extra)
		marker := asyncutil.HasMarker(extra)
		if trimmed == "" {
			parts = append(parts, fmt.Sprintf("extra[%d]=<empty>,marker=%v", idx, marker))
		} else {
			parts = append(parts, fmt.Sprintf("extra[%d]_len=%d,marker=%v", idx, len(trimmed), marker))
		}
	}
	return strings.Join(parts, ";")
}

func hasAsyncMarker(value string) bool {
	return asyncutil.HasMarker(value)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
