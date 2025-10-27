package sidebar

import "tui/internal/protocol"

// AgentState manages the current agent's information
type AgentState struct {
	Name        string
	Description string
	Color       string
	Commands    []protocol.CommandDescriptor
	Logs        []string
	List        []AgentListItem // List of available agents (for Opperator)
}

// NewAgentState creates a new AgentState
func NewAgentState() *AgentState {
	return &AgentState{
		Logs: make([]string, 0),
		List: make([]AgentListItem, 0),
	}
}

// SetInfo updates the agent's basic information
// Returns (changed bool, agentNameChanged bool)
func (a *AgentState) SetInfo(name, description, color string, commands []protocol.CommandDescriptor) (changed bool, agentNameChanged bool) {
	agentNameChanged = a.Name != name
	changed = agentNameChanged ||
		a.Description != description ||
		a.Color != color ||
		len(a.Commands) != len(commands)

	// If agent name is changing, clear logs from the previous agent
	if agentNameChanged {
		a.Logs = make([]string, 0)
	}

	a.Name = name
	a.Description = description
	a.Color = color
	a.Commands = commands

	return changed, agentNameChanged
}

// SetLogs updates the agent logs
func (a *AgentState) SetLogs(logs []string) (changed bool) {
	if stringSlicesEqual(a.Logs, logs) {
		return false
	}
	a.Logs = logs
	return true
}

// AppendLog adds a new log entry
func (a *AgentState) AppendLog(logEntry string) {
	a.Logs = append(a.Logs, logEntry)

	// Keep only the last 100 logs in memory for UI display
	const maxLogsInMemory = 100
	if len(a.Logs) > maxLogsInMemory {
		a.Logs = a.Logs[len(a.Logs)-maxLogsInMemory:]
	}
}

// SetList updates the agent list
func (a *AgentState) SetList(agents []AgentListItem) (changed bool) {
	if agentListEqual(a.List, agents) {
		return false
	}
	a.List = agents
	return true
}
