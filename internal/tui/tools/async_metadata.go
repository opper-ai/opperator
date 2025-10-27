package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

type TaskMetadata struct {
	TaskID   string
	Label    string
	Progress []string
}

// metadataParser caches JSON parsing results to avoid redundant work.
type metadataParser struct {
	cache map[string]map[string]any
}

func newMetadataParser() *metadataParser {
	return &metadataParser{cache: make(map[string]map[string]any)}
}

func (p *metadataParser) parse(raw string) map[string]any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if data, ok := p.cache[trimmed]; ok {
		return data
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		p.cache[trimmed] = nil
		return nil
	}
	p.cache[trimmed] = parsed
	return parsed
}

// ParseTaskMetadata extracts task ID, label, and progress from metadata JSON.
func ParseTaskMetadata(raw string) TaskMetadata {
	parser := newMetadataParser()
	data := parser.parse(raw)

	return TaskMetadata{
		TaskID:   extractAsyncTaskID(parser, data),
		Label:    extractProgressLabel(parser, data),
		Progress: extractProgressLines(parser, data),
	}
}

// extractAsyncTaskID finds the task ID in various possible locations.
func extractAsyncTaskID(parser *metadataParser, data map[string]any) string {
	if data == nil {
		return ""
	}

	// Try direct fields first
	if id := stringField(data, "async_task_id", "task_id", "id"); id != "" {
		return id
	}

	// Try nested objects
	if nested := nestedMap(parser, data, "async_task"); len(nested) > 0 {
		if id := stringField(nested, "id"); id != "" {
			return id
		}
	}
	if nested := nestedMap(parser, data, "task"); len(nested) > 0 {
		if id := stringField(nested, "id"); id != "" {
			return id
		}
	}

	// Try nested metadata strings
	if nestedRaw, ok := data["async_task_metadata"].(string); ok {
		if nested := parser.parse(nestedRaw); nested != nil {
			return extractAsyncTaskID(parser, nested)
		}
	}
	if nestedRaw, ok := data["metadata"].(string); ok {
		if nested := parser.parse(nestedRaw); nested != nil {
			return extractAsyncTaskID(parser, nested)
		}
	}

	return ""
}

// extractProgressLabel finds a human-readable label for the operation.
func extractProgressLabel(parser *metadataParser, data map[string]any) string {
	if data == nil {
		return ""
	}

	// Try direct fields
	if label := stringField(data, "progress_label", "async_task_label", "label", "command_label", "title", "name"); label != "" {
		return label
	}

	// Try nested objects
	if nested := nestedMap(parser, data, "async_task"); len(nested) > 0 {
		if label := stringField(nested, "label", "command_label", "title", "name"); label != "" {
			return label
		}
	}
	if nested := nestedMap(parser, data, "async_context"); len(nested) > 0 {
		if label := stringField(nested, "label", "title", "name"); label != "" {
			return label
		}
	}

	// Try nested metadata
	if nestedRaw, ok := data["async_task_metadata"].(string); ok {
		if nested := parser.parse(nestedRaw); nested != nil {
			return extractProgressLabel(parser, nested)
		}
	}

	return ""
}

// extractProgressLines finds progress message lines.
func extractProgressLines(parser *metadataParser, data map[string]any) []string {
	if data == nil {
		return nil
	}

	lines := collectProgressLines(parser, data)

	if nestedRaw, ok := data["async_task_metadata"].(string); ok {
		if nested := parser.parse(nestedRaw); nested != nil {
			lines = append(lines, extractProgressLines(parser, nested)...)
		}
	}

	return normalizeLines(lines)
}

// collectProgressLines extracts lines from a "progress" field.
func collectProgressLines(parser *metadataParser, m map[string]any) []string {
	if m == nil {
		return nil
	}

	value, ok := lookupInsensitive(m, "progress")
	if !ok {
		return nil
	}

	return collectProgressValue(parser, value)
}

// collectProgressValue handles different types of progress values.
func collectProgressValue(parser *metadataParser, value any) []string {
	switch v := value.(type) {
	case []any:
		lines := make([]string, 0, len(v))
		for _, item := range v {
			if entry, ok := item.(map[string]any); ok {
				if line := formatProgressMap(entry); line != "" {
					lines = append(lines, line)
				}
			}
		}
		return lines

	case map[string]any:
		if line := formatProgressMap(v); line != "" {
			return []string{line}
		}

	case string:
		if nested := parser.parse(v); nested != nil {
			return extractProgressLines(parser, nested)
		}
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return []string{trimmed}
		}
	}

	return nil
}

// formatProgressMap formats a progress entry map as a string.
func formatProgressMap(m map[string]any) string {
	if m == nil {
		return ""
	}

	text := stringField(m, "text", "message")
	status := stringField(m, "status")

	switch {
	case text != "" && status != "":
		return fmt.Sprintf("%s — %s", status, text)
	case text != "":
		return text
	case status != "":
		return status
	default:
		return ""
	}
}


func stringField(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := lookupInsensitive(m, key); ok {
			if str := stringFromAny(value); str != "" {
				return str
			}
		}
	}
	return ""
}

func nestedMap(parser *metadataParser, m map[string]any, key string) map[string]any {
	value, ok := lookupInsensitive(m, key)
	if !ok {
		return nil
	}

	switch t := value.(type) {
	case map[string]any:
		return t
	case string:
		return parser.parse(t)
	default:
		return nil
	}
}

func lookupInsensitive(m map[string]any, key string) (any, bool) {
	if m == nil {
		return nil, false
	}

	// Try exact match first
	if value, ok := m[key]; ok {
		return value, true
	}

	// Try case-insensitive match
	lower := strings.ToLower(strings.TrimSpace(key))
	for k, v := range m {
		if strings.ToLower(strings.TrimSpace(k)) == lower {
			return v, true
		}
	}

	return nil, false
}

func stringFromAny(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	case json.Number:
		return strings.TrimSpace(t.String())
	case []byte:
		return strings.TrimSpace(string(t))
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func normalizeLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}

	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}

	if len(filtered) == 0 {
		return nil
	}

	// Limit to max lines
	if len(filtered) > MaxAsyncProgressLines {
		start := len(filtered) - MaxAsyncProgressLines
		trimmed := make([]string, len(filtered)-start)
		copy(trimmed, filtered[start:])
		filtered = trimmed
	}

	return filtered
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
