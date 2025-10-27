package coreagent

import _ "embed"

var (
	//go:embed prompts/opperator.md
	promptOpperator string
	//go:embed prompts/builder.md
	promptBuilder string
)

var builtinPrompts = map[string]string{
	IDOpperator: promptOpperator,
	IDBuilder:   promptBuilder,
}
