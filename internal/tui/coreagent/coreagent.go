package coreagent

import (
	"slices"

	tooling "tui/tools"
)

const (
	// IDOpperator identifies the default Opperator persona.
	IDOpperator = "opperator"
	// IDBuilder identifies the built-in Builder persona.
	IDBuilder = "builder"
)

// without requiring an entry in agents.yaml.
type Definition struct {
	ID     string
	Name   string
	Prompt string
	Color  string
	Tools  []tooling.Spec
}

var definitions = map[string]Definition{
	IDOpperator: {
		ID:     IDOpperator,
		Name:   "Opperator",
		Prompt: builtinPrompts[IDOpperator],
		Color:  "#7f7f7f",
		Tools:  tooling.OpperatorSpecs(),
	},
	IDBuilder: {
		ID:     IDBuilder,
		Name:   "Builder",
		Prompt: builtinPrompts[IDBuilder],
		Color:  "#3ccad7",
		Tools:  tooling.BuilderSpecs(),
	},
}

func Default() Definition {
	return definitions[IDOpperator]
}

func Lookup(id string) (Definition, bool) {
	def, ok := definitions[id]
	if !ok {
		return Definition{}, false
	}
	clone := def
	clone.Tools = slices.Clone(def.Tools)
	return clone, true
}

func All() []Definition {
	preferred := []string{IDOpperator, IDBuilder}
	out := make([]Definition, 0, len(definitions))
	seen := make(map[string]struct{}, len(definitions))

	for _, id := range preferred {
		def, ok := definitions[id]
		if !ok {
			continue
		}
		clone := def
		clone.Tools = slices.Clone(def.Tools)
		out = append(out, clone)
		seen[id] = struct{}{}
	}

	extra := make([]Definition, 0, len(definitions)-len(seen))
	for id, def := range definitions {
		if _, ok := seen[id]; ok {
			continue
		}
		clone := def
		clone.Tools = slices.Clone(def.Tools)
		extra = append(extra, clone)
	}

	slices.SortFunc(extra, func(a, b Definition) int {
		if a.Name == b.Name {
			return 0
		}
		if a.Name < b.Name {
			return -1
		}
		return 1
	})

	return append(out, extra...)
}

func IDs() []string {
	ids := make([]string, 0, len(definitions))
	for id := range definitions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
