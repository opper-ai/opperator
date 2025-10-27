package toolstate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TranscriptEntry captures a single transcript record emitted by a tool.
type TranscriptEntry struct {
	Kind               string
	Status             string
	Content            string
	ToolName           string
	ToolInput          string
	ToolResultContent  string
	ToolResultMetadata string
}

// ProgressEntry captures a progress record emitted by a tool.
type ProgressEntry struct {
	Timestamp string
	Status    string
	Text      string
	Metadata  string
}

// ParseMetadata parses a raw metadata JSON string into a generic map.
func ParseMetadata(raw string) map[string]any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
		return nil
	}
	return data
}

// map or an empty string when no metadata is present.
func NormalizeMetadata(raw string) string {
	meta := ParseMetadata(raw)
	if len(meta) == 0 {
		return ""
	}
	return encodeMetadata(meta)
}

// MergeMetadata shallow-merges two metadata documents, concatenating
// transcript arrays when present. The returned JSON is normalized.
func MergeMetadata(base, update string) string {
	baseMeta := ParseMetadata(base)
	updateMeta := ParseMetadata(update)
	merged := mergeMetadataMaps(baseMeta, updateMeta)
	if len(merged) == 0 {
		return ""
	}
	return encodeMetadata(merged)
}

// TranscriptEntries extracts transcript entries from metadata.
func TranscriptEntries(meta map[string]any) []TranscriptEntry {
	items := normalizeToSlice(meta, "transcript")
	entries := make([]TranscriptEntry, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entries = append(entries, TranscriptEntry{
			Kind:               extractString(m, "kind"),
			Status:             extractString(m, "status"),
			Content:            extractString(m, "content"),
			ToolName:           extractString(m, "tool_name"),
			ToolInput:          extractString(m, "tool_input"),
			ToolResultContent:  extractString(m, "tool_result_content"),
			ToolResultMetadata: extractString(m, "tool_result_metadata"),
		})
	}
	return entries
}

// ProgressEntries extracts progress entries from metadata.
func ProgressEntries(meta map[string]any) []ProgressEntry {
	items := normalizeToSlice(meta, "progress")
	entries := make([]ProgressEntry, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entries = append(entries, ProgressEntry{
			Timestamp: extractString(m, "timestamp"),
			Status:    extractString(m, "status"),
			Text:      extractString(m, "text"),
			Metadata:  extractString(m, "metadata"),
		})
	}
	return entries
}

// ExtractAgentMeta pulls common agent metadata fields from a metadata map.
func ExtractAgentMeta(meta map[string]any) (agentName, taskDefinition string) {
	if meta == nil {
		return "", ""
	}
	return extractString(meta, "agent_name", "agent", "AgentName"),
		extractString(meta, "task_definition", "TaskDefinition")
}

func mergeMetadataMaps(base, update map[string]any) map[string]any {
	if len(base) == 0 {
		return cloneMap(update)
	}
	if len(update) == 0 {
		return cloneMap(base)
	}

	merged := cloneMap(base)

	baseTranscript := extractAnySlice(base["transcript"])
	updateTranscript := extractAnySlice(update["transcript"])
	if len(baseTranscript) > 0 || len(updateTranscript) > 0 {
		merged["transcript"] = append(cloneSlice(baseTranscript), cloneSlice(updateTranscript)...)
	}

	for k, v := range update {
		if equalNormalizedKey(k, "transcript") {
			continue
		}
		merged[k] = cloneValue(v)
	}

	return merged
}

func encodeMetadata(meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return ""
	}
	return string(b)
}

func normalizeToSlice(meta map[string]any, key string) []any {
	if meta == nil {
		return nil
	}
	val, ok := meta[key]
	if !ok {
		return nil
	}
	return extractAnySlice(val)
}

func extractAnySlice(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case []map[string]any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
		return out
	case map[string]any:
		return []any{v}
	default:
		return nil
	}
}

func extractString(m map[string]any, keys ...string) string {
	if m == nil {
		return ""
	}
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

func equalNormalizedKey(a, b string) bool {
	return normalizeKey(a) == normalizeKey(b)
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneSlice(src []any) []any {
	if len(src) == 0 {
		return nil
	}
	out := make([]any, len(src))
	for i := range src {
		out[i] = cloneValue(src[i])
	}
	return out
}

func cloneValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return cloneMap(val)
	case []any:
		return cloneSlice(val)
	default:
		return val
	}
}
