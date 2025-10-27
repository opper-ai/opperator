package tools

import (
	"strings"

	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
)

func definitionLabel(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if def, ok := toolregistry.Lookup(trimmed); ok {
		if label := strings.TrimSpace(def.Label); label != "" {
			return label
		}
	}
	return ""
}

// isFallbackAsyncLabel reports whether candidate is a generic fallback label for
func isFallbackAsyncLabel(candidate string, call tooltypes.Call, result tooltypes.Result) bool {
	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		return true
	}
	if strings.EqualFold(trimmed, "Async") {
		return true
	}
	asyncNames := []string{
		strings.TrimSpace(call.Name),
		strings.TrimSpace(result.Name),
	}
	for _, name := range asyncNames {
		if !isGenericAsyncName(name) {
			continue
		}
		fallback := toolregistry.PrettifyIdentifier(name)
		if fallback != "" && strings.EqualFold(trimmed, fallback) {
			return true
		}
	}
	return false
}

func isGenericAsyncName(name string) bool {
	lowered := strings.ToLower(strings.TrimSpace(name))
	if lowered == "" {
		return false
	}
	if lowered == strings.ToLower(AsyncToolName) {
		return true
	}
	if lowered == "async" {
		return true
	}
	if strings.HasPrefix(lowered, "async_") {
		return true
	}
	return false
}

// preferring metadata-derived labels and registry definitions over generic
// fallbacks.
func PreferredAsyncLabel(call tooltypes.Call, result tooltypes.Result) string {
	parser := newMetadataParser()

	for _, raw := range []string{result.Metadata, call.Input, call.Reason} {
		if label := strings.TrimSpace(extractProgressLabel(parser, parser.parse(raw))); label != "" {
			return label
		}
	}

	if label := definitionLabel(result.Name); label != "" {
		return label
	}
	if label := definitionLabel(call.Name); label != "" {
		return label
	}
	return ""
}
