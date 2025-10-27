package registry

import (
	"strings"
	"sync"

	"tui/tools/types"
)

type Definition struct {
	Label             string
	Pending           func(call types.Call, width int, spinner string) string
	PendingWithResult func(call types.Call, result types.Result, width int, spinner string) string
	Render            func(call types.Call, result types.Result, width int) string
	SummaryRender     func(call types.Call, result types.Result, width int) string
	Copy              func(call types.Call, result types.Result) string
	Hidden            bool // If true, tool calls will not be displayed in the TUI
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Definition{}
)

func Register(name string, def Definition) {
	if name == "" {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[strings.ToLower(name)] = def
}

// Lookup retrieves the registered definition for name, if any.
func Lookup(name string) (Definition, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	def, ok := registry[strings.ToLower(name)]
	return def, ok
}

func PrettifyName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if def, ok := Lookup(trimmed); ok {
		if label := strings.TrimSpace(def.Label); label != "" {
			return label
		}
	}
	return prettifyIdentifier(trimmed)
}

// PrettifyIdentifier converts a raw identifier into a human-friendly form by
// replacing underscores with spaces and capitalizing words. Unlike
// PrettifyName it never considers registered labels.
func PrettifyIdentifier(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	return prettifyIdentifier(trimmed)
}

func prettifyIdentifier(name string) string {
	name = strings.ReplaceAll(name, "_", " ")
	parts := strings.Fields(name)
	for i, part := range parts {
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		runes := []rune(lower)
		if len(runes) == 0 {
			continue
		}
		first := strings.ToUpper(string(runes[0]))
		parts[i] = first + string(runes[1:])
	}
	return strings.Join(parts, " ")
}
