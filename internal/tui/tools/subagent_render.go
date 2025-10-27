package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
	"tui/styles"
	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
)

type subAgentSummary struct {
	Status             string
	ToolCallID         string
	ToolName           string
	ToolInput          string
	ToolResultContent  string
	ToolResultMetadata string
}

type subAgentCallDetails struct {
	TaskDefinition string
	AgentName      string
}

type subAgentMetadata struct {
	TaskDefinition string
	AgentName      string
	Transcript     []transcriptEvent
}

func init() {
	toolregistry.Register("agent", toolregistry.Definition{
		Label: "Agent",
		PendingWithResult: func(call tooltypes.Call, result tooltypes.Result, width int, spinner string) string {
			return renderSubAgent(call, result, width, spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			return renderSubAgent(call, result, width, "")
		},
	})
}

func renderSubAgent(call tooltypes.Call, result tooltypes.Result, width int, spinner string) string {
	t := styles.CurrentTheme()
	meta := parseAgentMetadata(result.Metadata)
	callDetails := parseAgentCallInput(call.Input)

	taskDefinition := strings.TrimSpace(meta.TaskDefinition)
	if taskDefinition == "" {
		taskDefinition = callDetails.TaskDefinition
	}
	agentName := strings.TrimSpace(meta.AgentName)
	if agentName == "" {
		agentName = callDetails.AgentName
	}
	agentName = formatAgentDisplay(agentName)

	title := "Agent"
	if agentName != "" {
		title = fmt.Sprintf("Agent (%s)", agentName)
	}
	if td := strings.TrimSpace(taskDefinition); td != "" {
		if agentName != "" {
			title = fmt.Sprintf("Agent (%s) — %s", agentName, td)
		} else {
			title = fmt.Sprintf("Agent — %s", td)
		}
	}
	headerBase := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + title)

	indent := "  "
	gutter := lipgloss.NewStyle().Foreground(t.White).Render("│ ")
	prefix := indent + gutter
	contentWidth := width
	if contentWidth <= 0 {
		contentWidth = 80
	}
	prefixWidth := lipgloss.Width(prefix)
	if contentWidth < prefixWidth+8 {
		contentWidth = prefixWidth + 8
	}
	maxLine := contentWidth - prefixWidth

	display := make([]subAgentSummary, 0, len(meta.Transcript))
	index := make(map[string]int, len(meta.Transcript))

	for _, ev := range meta.Transcript {
		if strings.ToLower(ev.Kind) != "tool" {
			continue
		}
		id := strings.TrimSpace(ev.CallUID)
		if id == "" {
			id = strings.TrimSpace(ev.ToolCallID)
		}
		if id == "" {
			id = fmt.Sprintf("%s|%s", ev.ToolName, ev.ToolInput)
		}
		s := subAgentSummary{
			Status:             ev.Status,
			ToolCallID:         ev.ToolCallID,
			ToolName:           ev.ToolName,
			ToolInput:          ev.ToolInput,
			ToolResultContent:  ev.ToolResultContent,
			ToolResultMetadata: ev.ToolResultMetadata,
		}
		if pos, ok := index[id]; ok {
			prev := display[pos]
			if strings.TrimSpace(s.ToolName) == "" {
				s.ToolName = prev.ToolName
			}
			if strings.TrimSpace(s.ToolInput) == "" {
				s.ToolInput = prev.ToolInput
			}
			if strings.TrimSpace(s.ToolResultContent) == "" {
				s.ToolResultContent = prev.ToolResultContent
			}
			if strings.TrimSpace(s.ToolResultMetadata) == "" {
				s.ToolResultMetadata = prev.ToolResultMetadata
			}
			if strings.TrimSpace(s.Status) == "" {
				s.Status = prev.Status
			}
			display[pos] = s
		} else {
			index[id] = len(display)
			display = append(display, s)
		}
	}

	hasPending := false
	for _, entry := range display {
		status := strings.ToLower(strings.TrimSpace(entry.Status))
		if status != "" && status != "finish" {
			hasPending = true
			break
		}
	}
	if !hasPending {
		spinner = ""
	}

	header := headerBase
	if spinner != "" {
		header += " " + spinner
	}

	var builder strings.Builder
	builder.WriteString(header)

	for i, entry := range display {
		tc := tooltypes.Call{ID: entry.ToolCallID, Name: entry.ToolName, Input: entry.ToolInput, Finished: true}
		tr := tooltypes.Result{ToolCallID: entry.ToolCallID, Name: entry.ToolName, Content: entry.ToolResultContent, Metadata: entry.ToolResultMetadata}
		label := renderToolSummary(entry, tc, tr, maxLine)
		if strings.TrimSpace(label) == "" {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(entry.Status))
		switch status {
		case "start":
			label = lipgloss.NewStyle().Foreground(t.FgMuted).Render("… ") + label
		case "finish":
			if isFailureSummary(entry) {
				label = lipgloss.NewStyle().Foreground(t.Error).Render("✗ ") + label
			} else {
				label = lipgloss.NewStyle().Foreground(t.Success).Render("✓ ") + label
			}
		}
		if i == 0 {
			builder.WriteString("\n\n")
		} else {
			builder.WriteString("\n")
		}
		builder.WriteString(prefix)
		builder.WriteString(truncateWidth(label, maxLine))
	}

	finalContent := strings.TrimSpace(result.Content)
	if strings.EqualFold(finalContent, "canceled") {
		finalContent = "Request has been cancelled."
	}
	if finalContent == "" {
		finalContent = failureMessage(display)
	}
	if finalContent != "" {
		if len(display) == 0 {
			builder.WriteString("\n\n")
		} else {
			builder.WriteString("\n")
		}
		builder.WriteString(prefix)
		builder.WriteString(truncateWidth(lipgloss.NewStyle().Foreground(t.FgMuted).Italic(true).Render(finalContent), maxLine))
	}

	return builder.String()
}

func parseAgentCallInput(input string) subAgentCallDetails {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return subAgentCallDetails{}
	}
	var payload struct {
		TaskDefinition string `json:"task_definition"`
		AltTaskDef     string `json:"taskDefinition"`
		AgentName      string `json:"agent"`
		AltAgentName   string `json:"agent_name"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return subAgentCallDetails{}
	}
	var details subAgentCallDetails
	if td := strings.TrimSpace(payload.TaskDefinition); td != "" {
		details.TaskDefinition = td
	} else if td := strings.TrimSpace(payload.AltTaskDef); td != "" {
		details.TaskDefinition = td
	}
	if name := strings.TrimSpace(payload.AgentName); name != "" {
		details.AgentName = name
	} else if name := strings.TrimSpace(payload.AltAgentName); name != "" {
		details.AgentName = name
	}
	return details
}

func formatAgentDisplay(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if strings.EqualFold(trimmed, "builder") {
		return "Builder"
	}
	return trimmed
}

func renderToolSummary(entry subAgentSummary, call tooltypes.Call, result tooltypes.Result, maxWidth int) string {
	if def, ok := toolregistry.Lookup(entry.ToolName); ok && def.SummaryRender != nil {
		if summary := strings.TrimSpace(def.SummaryRender(call, result, maxWidth)); summary != "" {
			return summary
		}
	}
	if label := strings.TrimSpace(toolregistry.PrettifyName(entry.ToolName)); label != "" {
		return label
	}
	return strings.TrimSpace(entry.ToolResultContent)
}

func failureMessage(entries []subAgentSummary) string {
	for _, entry := range entries {
		if !isFailureSummary(entry) {
			continue
		}
		if msg := strings.TrimSpace(entry.ToolResultContent); msg != "" {
			return msg
		}
		if meta := strings.TrimSpace(entry.ToolResultMetadata); meta != "" {
			if decoded := extractFailureMessage(meta); decoded != "" {
				return decoded
			}
		}
	}
	return ""
}

func isFailureSummary(entry subAgentSummary) bool {
	content := strings.ToLower(strings.TrimSpace(entry.ToolResultContent))
	if strings.HasPrefix(content, "error") || strings.Contains(content, "permission denied") {
		return true
	}
	meta := strings.TrimSpace(entry.ToolResultMetadata)
	if meta == "" {
		return false
	}
	var payload any
	if err := json.Unmarshal([]byte(meta), &payload); err != nil {
		return false
	}
	return containsFailureFlag(payload)
}

func extractFailureMessage(meta string) string {
	var payload any
	if err := json.Unmarshal([]byte(meta), &payload); err != nil {
		return ""
	}
	return findErrorMessage(payload)
}

func findErrorMessage(node any) string {
	switch v := node.(type) {
	case map[string]any:
		if msg, ok := v["error"].(string); ok && strings.TrimSpace(msg) != "" {
			return msg
		}
		if msg, ok := v["message"].(string); ok && strings.TrimSpace(msg) != "" {
			return msg
		}
		for _, child := range v {
			if result := findErrorMessage(child); result != "" {
				return result
			}
		}
	case []any:
		for _, child := range v {
			if result := findErrorMessage(child); result != "" {
				return result
			}
		}
	}
	return ""
}

func containsFailureFlag(node any) bool {
	switch v := node.(type) {
	case map[string]any:
		if raw, ok := v["success"]; ok {
			if b, ok := raw.(bool); ok {
				return !b
			}
		}
		for _, child := range v {
			if containsFailureFlag(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsFailureFlag(child) {
				return true
			}
		}
	}
	return false
}

type transcriptEvent struct {
	Kind               string
	Status             string
	Content            string
	ToolCallID         string
	CallUID            string
	ToolName           string
	ToolInput          string
	ToolResultContent  string
	ToolResultMetadata string
}

func parseAgentMetadata(meta string) subAgentMetadata {
	meta = strings.TrimSpace(meta)
	if meta == "" {
		return subAgentMetadata{}
	}
	var root any
	if err := json.Unmarshal([]byte(meta), &root); err != nil {
		return subAgentMetadata{}
	}
	data, ok := root.(map[string]any)
	if !ok {
		return subAgentMetadata{}
	}
	info := subAgentMetadata{}
	info.TaskDefinition = extractString(data, "task_definition", "TaskDefinition", "taskDefinition")
	info.AgentName = formatAgentDisplay(extractString(data, "agent_name", "AgentName", "agent", "Agent", "agent_identifier", "AgentIdentifier"))
	if raw, ok := data["transcript"]; ok {
		info.Transcript = append(info.Transcript, parseTranscript(raw)...)
	}
	if raw, ok := data["Transcript"]; ok {
		info.Transcript = append(info.Transcript, parseTranscript(raw)...)
	}
	return info
}

func parseTranscript(raw any) []transcriptEvent {
	switch v := raw.(type) {
	case []any:
		return buildTranscriptEvents(v)
	case []map[string]any:
		items := make([]any, len(v))
		for i := range v {
			items[i] = v[i]
		}
		return buildTranscriptEvents(items)
	case map[string]any:
		return buildTranscriptEvents([]any{v})
	default:
		return nil
	}
}

func buildTranscriptEvents(items []any) []transcriptEvent {
	var events []transcriptEvent
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		events = append(events, transcriptEvent{
			Kind:               extractString(entry, "kind", "Kind"),
			Status:             extractString(entry, "status", "Status"),
			Content:            extractString(entry, "content", "Content"),
			ToolCallID:         extractString(entry, "tool_call_id", "ToolCallID"),
			CallUID:            extractString(entry, "call_uid", "CallUID"),
			ToolName:           extractString(entry, "tool_name", "ToolName"),
			ToolInput:          extractString(entry, "tool_input", "ToolInput"),
			ToolResultContent:  extractString(entry, "tool_result_content", "ToolResultContent"),
			ToolResultMetadata: extractString(entry, "tool_result_metadata", "ToolResultMetadata"),
		})
	}
	return events
}

func extractString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if val, ok := m[key]; ok {
			if s := stringify(val); s != "" {
				return s
			}
		}
	}
	normalized := make(map[string]any, len(m))
	for k, v := range m {
		normalized[normalizeKey(k)] = v
	}
	for _, key := range keys {
		if val, ok := normalized[normalizeKey(key)]; ok {
			if s := stringify(val); s != "" {
				return s
			}
		}
	}
	return ""
}

func stringify(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	default:
		if b, err := json.Marshal(val); err == nil {
			s := string(b)
			if s == "null" {
				return ""
			}
			return s
		}
		return fmt.Sprint(val)
	}
}

func normalizeKey(s string) string {
	replaced := strings.ReplaceAll(s, "_", "")
	return strings.ToLower(replaced)
}
