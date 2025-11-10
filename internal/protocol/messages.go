package protocol

import (
	"encoding/json"
	"strings"
	"time"
	"unicode"
)

type MessageType string

const (
	// Lifecycle messages
	MsgReady MessageType = "ready"

	// Logging messages
	MsgLog MessageType = "log"

	// Event messages
	MsgEvent MessageType = "event"

	// Lifecycle event messages (manager → process)
	MsgLifecycleEvent MessageType = "lifecycle_event"

	// Command messages
	MsgCommand          MessageType = "command"
	MsgResponse         MessageType = "response"
	MsgCommandRegistry  MessageType = "command_registry"
	MsgSystemPrompt     MessageType = "system_prompt"
	MsgAgentDescription MessageType = "agent_description"
	MsgCommandProgress  MessageType = "command_progress"

	// Sidebar messages
	MsgSidebarSection        MessageType = "sidebar_section"
	MsgSidebarSectionRemoval MessageType = "sidebar_section_removal"

	// Error messages
	MsgError MessageType = "error"
)

type LogLevel string

const (
	LogDebug   LogLevel = "debug"
	LogInfo    LogLevel = "info"
	LogWarning LogLevel = "warning"
	LogError   LogLevel = "error"
	LogFatal   LogLevel = "fatal"
)

// Message is the base structure for all messages
type Message struct {
	Type      MessageType     `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// ReadyMessage sent when process is ready
type ReadyMessage struct {
	PID     int    `json:"pid"`
	Version string `json:"version,omitempty"`
}

type LogMessage struct {
	Level   LogLevel               `json:"level"`
	Message string                 `json:"message"`
	Fields  map[string]interface{} `json:"fields,omitempty"`
}

type EventMessage struct {
	Name string                 `json:"name"`
	Data map[string]interface{} `json:"data,omitempty"`
}

type LifecycleEventMessage struct {
	EventType string                 `json:"event_type"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// CommandMessage sent from manager to process
type CommandMessage struct {
	Command    string                 `json:"command"`
	Args       map[string]interface{} `json:"args,omitempty"`
	ID         string                 `json:"id,omitempty"`
	WorkingDir string                 `json:"working_dir,omitempty"`
}

// ResponseMessage sent in response to commands
type ResponseMessage struct {
	CommandID string      `json:"command_id,omitempty"`
	Success   bool        `json:"success"`
	Result    interface{} `json:"result,omitempty"`
	Error     string      `json:"error,omitempty"`
}

// SystemPromptMessage allows an agent to publish its system prompt to the
// manager.
type SystemPromptMessage struct {
	Prompt  string `json:"prompt"`
	Replace bool   `json:"replace,omitempty"`
}

// AgentDescriptionMessage allows an agent to publish its human-readable description.
type AgentDescriptionMessage struct {
	Description string `json:"description"`
}

// SidebarSectionMessage allows an agent to register/update custom sidebar sections.
type SidebarSectionMessage struct {
	SectionID string `json:"section_id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Collapsed bool   `json:"collapsed"`
}

// SidebarSectionRemovalMessage allows an agent to remove a custom sidebar section.
type SidebarSectionRemovalMessage struct {
	SectionID string `json:"section_id"`
}

// CommandExposure indicates how a command should be exposed to users.
type CommandExposure string

const (
	CommandExposureAgentTool    CommandExposure = "agent_tool"
	CommandExposureSlashCommand CommandExposure = "slash_command"
)

type CommandDescriptor struct {
	Name             string            `json:"name"`
	Title            string            `json:"title,omitempty"`
	Description      string            `json:"description,omitempty"`
	ExposeAs         []CommandExposure `json:"expose_as,omitempty"`
	SlashCommand     string            `json:"slash_command,omitempty"`
	SlashScope       string            `json:"slash_scope,omitempty"`
	ArgumentHint     string            `json:"argument_hint,omitempty"`
	ArgumentRequired bool              `json:"argument_required,omitempty"`
	Arguments        []CommandArgument `json:"arguments,omitempty"`
	Async            bool              `json:"async,omitempty"`
	ProgressLabel    string            `json:"progress_label,omitempty"`
	Hidden           bool              `json:"hidden,omitempty"`
}

// CommandProgressMessage emits incremental updates for a long-running command.
type CommandProgressMessage struct {
	CommandID string                 `json:"command_id,omitempty"`
	Text      string                 `json:"text,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Status    string                 `json:"status,omitempty"`
	Progress  float64                `json:"progress,omitempty"`
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

// CommandRegistryMessage announces available commands from the agent
type CommandRegistryMessage struct {
	Commands []CommandDescriptor `json:"commands"`
}

type ErrorMessage struct {
	Error   string `json:"error"`
	Code    int    `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

func NewMessage(msgType MessageType, data interface{}) (*Message, error) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	return &Message{
		Type:      msgType,
		Timestamp: time.Now(),
		Data:      dataBytes,
	}, nil
}

// ParseMessage parses a JSON message
func ParseMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// ExtractData extracts the data field into the provided interface
func (m *Message) ExtractData(v interface{}) error {
	if m.Data == nil {
		return nil
	}
	return json.Unmarshal(m.Data, v)
}

// ToJSON converts the message to JSON
func (m *Message) ToJSON() ([]byte, error) {
	return json.Marshal(m)
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

func normalizeSlashScope(scope string) string {
	trimmed := strings.TrimSpace(scope)
	if strings.EqualFold(trimmed, "global") {
		return "global"
	}
	return "local"
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
		if _, duplicate := seen[name]; duplicate {
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
