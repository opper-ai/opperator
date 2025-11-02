package sidebar

// Section represents a sidebar section type
type Section int

const (
	SectionNone         Section = -1 // No built-in section selected (used when in custom sections)
	SectionAgentInfo    Section = iota
	SectionAgents               // Opperator: list of available agents
	SectionFocusedAgent         // Builder: currently focused agent
	SectionTodos                // Builder: todos for focused agent
	SectionTools
	SectionSlashCommands
	SectionLogs
)

// AgentListItem represents an agent in the agents list
type AgentListItem struct {
	Name        string
	Description string
	Status      string
	Color       string
	Daemon      string // Which daemon this agent is on
}

// CustomSection represents a custom sidebar section
type CustomSection struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Collapsed bool   `json:"collapsed"`
}

// TodoItem represents a todo item
type TodoItem struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Completed bool   `json:"completed"`
}
