package sidebar

import "tui/internal/protocol"

// BuilderState manages Builder-specific state (focused agent and todos)
type BuilderState struct {
	FocusedAgentName        string
	FocusedAgentStatus      string
	FocusedAgentDescription string
	FocusedAgentColor       string
	FocusedAgentCommands    []protocol.CommandDescriptor
	Todos                   []TodoItem
}

// NewBuilderState creates a new BuilderState
func NewBuilderState() *BuilderState {
	return &BuilderState{
		Todos: make([]TodoItem, 0),
	}
}

// SetFocusedAgent updates the focused agent name
func (b *BuilderState) SetFocusedAgent(name string) (changed bool) {
	if b.FocusedAgentName != name {
		b.FocusedAgentName = name
		return true
	}
	return false
}

// SetFocusedAgentStatus updates the focused agent status
func (b *BuilderState) SetFocusedAgentStatus(status string) (changed bool) {
	if b.FocusedAgentStatus != status {
		b.FocusedAgentStatus = status
		return true
	}
	return false
}

// SetFocusedAgentDescription updates the focused agent description
func (b *BuilderState) SetFocusedAgentDescription(description string) (changed bool) {
	if b.FocusedAgentDescription != description {
		b.FocusedAgentDescription = description
		return true
	}
	return false
}

// SetFocusedAgentColor updates the focused agent color
func (b *BuilderState) SetFocusedAgentColor(color string) (changed bool) {
	if b.FocusedAgentColor != color {
		b.FocusedAgentColor = color
		return true
	}
	return false
}

// SetFocusedAgentCommands updates the focused agent commands
func (b *BuilderState) SetFocusedAgentCommands(commands []protocol.CommandDescriptor) (changed bool) {
	if len(b.FocusedAgentCommands) != len(commands) {
		b.FocusedAgentCommands = commands
		return true
	}

	for i, cmd := range commands {
		if i >= len(b.FocusedAgentCommands) || b.FocusedAgentCommands[i].Name != cmd.Name {
			b.FocusedAgentCommands = commands
			return true
		}
	}

	return false
}

// SetTodos updates the todo list
func (b *BuilderState) SetTodos(todos []TodoItem) (changed bool) {
	if todosEqual(b.Todos, todos) {
		return false
	}
	b.Todos = todos
	return true
}
