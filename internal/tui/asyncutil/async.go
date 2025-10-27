package asyncutil

import (
	"strings"

	tooling "tui/tools"
	tooltypes "tui/tools/types"
)

// HasMarker reports whether the provided string contains a marker indicating async execution.
func HasMarker(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "\"async_task\"") {
		return true
	}
	if strings.Contains(trimmed, "async task") {
		return true
	}
	return strings.Contains(trimmed, strings.ToLower(tooling.AsyncToolName))
}

func IsContext(id, name string, extras ...string) bool {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(id)), "async_") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(name), tooling.AsyncToolName) {
		return true
	}
	for _, extra := range extras {
		if HasMarker(extra) {
			return true
		}
	}
	return false
}

// IsCall infers async status directly from a tool call and optional context strings.
func IsCall(call tooltypes.Call, extras ...string) bool {
	items := append([]string{call.Input, call.Reason}, extras...)
	return IsContext(call.ID, call.Name, items...)
}

// IsResult infers async status from a tool result payload.
func IsResult(result tooltypes.Result, extras ...string) bool {
	items := append([]string{result.Metadata, result.Content}, extras...)
	return IsContext(result.ToolCallID, result.Name, items...)
}
