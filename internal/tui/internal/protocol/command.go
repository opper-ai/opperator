package protocol

import (
	"strings"
	"unicode"
)

// CommandExposure indicates how a command should be exposed to users.
type CommandExposure string

const (
	CommandExposureAgentTool    CommandExposure = "agent_tool"
	CommandExposureSlashCommand CommandExposure = "slash_command"
)

// SlashCommandScope controls where a slash command should appear.
type SlashCommandScope string

const (
	SlashCommandScopeLocal  SlashCommandScope = "local"
	SlashCommandScopeGlobal SlashCommandScope = "global"
)

type CommandDescriptor struct {
	Name             string            `json:"name"`
	Title            string            `json:"title,omitempty"`
	Description      string            `json:"description,omitempty"`
	ExposeAs         []CommandExposure `json:"expose_as,omitempty"`
	SlashCommand     string            `json:"slash_command,omitempty"`
	SlashScope       SlashCommandScope `json:"slash_scope,omitempty"`
	ArgumentHint     string            `json:"argument_hint,omitempty"`
	ArgumentRequired bool              `json:"argument_required,omitempty"`
	Arguments        []CommandArgument `json:"arguments,omitempty"`
	Async            bool              `json:"async,omitempty"`
	ProgressLabel    string            `json:"progress_label,omitempty"`
}

type CommandArgument struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type,omitempty"`
	Description string                 `json:"description,omitempty"`
	Required    bool                   `json:"required,omitempty"`
	Default     interface{}            `json:"default,omitempty"`
	Enum        []interface{}          `json:"enum,omitempty"`
	Items       map[string]interface{} `json:"items,omitempty"`      // Schema for array items
	Properties  map[string]interface{} `json:"properties,omitempty"` // Schema for object properties
}

func NormalizeCommandDescriptors(defs []CommandDescriptor) []CommandDescriptor {
	if len(defs) == 0 {
		return nil
	}
	normalized := make([]CommandDescriptor, 0, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		def.Name = name

		title := strings.TrimSpace(def.Title)
		if title == "" {
			title = deriveCommandTitle(name)
		}
		if title == "" {
			title = name
		}
		def.Title = title

		def.Description = strings.TrimSpace(def.Description)
		def.ProgressLabel = strings.TrimSpace(def.ProgressLabel)

		exposures := normalizeExposures(def.ExposeAs)
		slash := normalizeSlashCommand(def.SlashCommand)
		if slash != "" {
			if !containsExposure(exposures, CommandExposureSlashCommand) {
				exposures = append(exposures, CommandExposureSlashCommand)
			}
		} else if containsExposure(exposures, CommandExposureSlashCommand) {
			slash = normalizeSlashCommand(def.Name)
		}
		def.ExposeAs = exposures
		def.SlashCommand = slash
		def.SlashScope = normalizeSlashScope(def.SlashScope)
		def.ArgumentHint = strings.TrimSpace(def.ArgumentHint)
		def.Arguments = normalizeCommandArguments(def.Arguments)

		normalized = append(normalized, def)
	}
	return normalized
}

func normalizeExposures(values []CommandExposure) []CommandExposure {
	seen := make(map[CommandExposure]struct{})
	normalized := make([]CommandExposure, 0, len(values))
	for _, value := range values {
		exposure := CommandExposure(strings.TrimSpace(string(value)))
		if exposure != CommandExposureAgentTool && exposure != CommandExposureSlashCommand {
			continue
		}
		if _, exists := seen[exposure]; exists {
			continue
		}
		normalized = append(normalized, exposure)
		seen[exposure] = struct{}{}
	}
	if len(normalized) == 0 {
		normalized = append(normalized, CommandExposureAgentTool)
	}
	return normalized
}

func normalizeSlashCommand(cmd string) string {
	trimmed := strings.TrimSpace(cmd)
	trimmed = strings.TrimLeft(trimmed, "/")
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.WriteByte('/')
	lastUnderscore := false
	for _, r := range trimmed {
		switch {
		case r == '/' || r == '\\':
			continue
		case r == '-' || r == ':':
			b.WriteRune(r)
			lastUnderscore = false
		case r == '_' || r == '.':
			if !lastUnderscore {
				b.WriteRune('_')
				lastUnderscore = true
			}
		case r == ' ' || r == '\t':
			if !lastUnderscore {
				b.WriteRune('_')
				lastUnderscore = true
			}
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			lastUnderscore = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteRune('_')
				lastUnderscore = true
			}
		}
	}
	out := b.String()
	out = strings.TrimRight(out, "_")
	if out == "/" {
		return ""
	}
	return out
}

func normalizeSlashScope(scope SlashCommandScope) SlashCommandScope {
	trimmed := strings.TrimSpace(string(scope))
	switch strings.ToLower(trimmed) {
	case string(SlashCommandScopeGlobal):
		return SlashCommandScopeGlobal
	default:
		return SlashCommandScopeLocal
	}
}

func normalizeCommandArguments(args []CommandArgument) []CommandArgument {
	if len(args) == 0 {
		return nil
	}
	allowed := map[string]struct{}{
		"string":  {},
		"integer": {},
		"number":  {},
		"boolean": {},
		"array":   {},
		"object":  {},
	}
	seen := make(map[string]struct{})
	normalized := make([]CommandArgument, 0, len(args))
	for _, arg := range args {
		name := strings.TrimSpace(arg.Name)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		typeName := strings.ToLower(strings.TrimSpace(arg.Type))
		if typeName == "" {
			typeName = "string"
		}
		if _, ok := allowed[typeName]; !ok {
			typeName = "string"
		}
		description := strings.TrimSpace(arg.Description)
		enumValues := make([]interface{}, 0, len(arg.Enum))
		for _, value := range arg.Enum {
			if value == nil {
				continue
			}
			enumValues = append(enumValues, value)
		}
		if len(enumValues) == 0 {
			enumValues = nil
		}
		normalized = append(normalized, CommandArgument{
			Name:        name,
			Type:        typeName,
			Description: description,
			Required:    arg.Required,
			Default:     arg.Default,
			Enum:        enumValues,
			Items:       arg.Items,
			Properties:  arg.Properties,
		})
		seen[name] = struct{}{}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func containsExposure(exposures []CommandExposure, target CommandExposure) bool {
	for _, exposure := range exposures {
		if exposure == target {
			return true
		}
	}
	return false
}

func deriveCommandTitle(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}

	var words []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		words = append(words, string(current))
		current = current[:0]
	}

	var last rune
	for _, r := range trimmed {
		switch {
		case r == '_' || r == '-' || r == ':' || r == '.':
			flush()
			last = 0
			continue
		case unicode.IsSpace(r):
			flush()
			last = 0
			continue
		}

		if len(current) > 0 {
			if unicode.IsUpper(r) && (unicode.IsLower(last) || unicode.IsDigit(last)) {
				flush()
			} else if unicode.IsDigit(r) && !unicode.IsDigit(last) {
				flush()
			}
		}

		current = append(current, unicode.ToLower(r))
		last = r
	}
	flush()

	if len(words) == 0 {
		return trimmed
	}

	for i, word := range words {
		runes := []rune(word)
		if len(runes) == 0 {
			continue
		}
		if unicode.IsLetter(runes[0]) {
			runes[0] = unicode.ToUpper(runes[0])
		}
		words[i] = string(runes)
	}

	return strings.Join(words, " ")
}
