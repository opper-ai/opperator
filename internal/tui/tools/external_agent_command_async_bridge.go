package tools

import (
	"strings"

	toolregistry "tui/tools/registry"
)

// registerExternalAgentCommandAsyncRenderer bridges async agent commands to the
// shared async renderer while preserving per-command labels.
func registerExternalAgentCommandAsyncRenderer(def externalAgentCommandDef) {
	label := strings.TrimSpace(def.Label)
	if label == "" {
		label = "Async"
	}
	toolregistry.Register(def.ToolName, AsyncToolDefinition(label))
}
