package sidebar

import "context"

// PreferencesStore defines the interface for storing and retrieving sidebar preferences
type PreferencesStore interface {
	GetBool(ctx context.Context, key string) (bool, error)
	SetBool(ctx context.Context, key string, value bool) error
}

// Preference keys for sidebar state persistence
const (
	prefKeyVisible          = "sidebar.visible"
	prefKeyAgentInfo        = "sidebar.agent_info.expanded"
	prefKeyAgents           = "sidebar.agents.expanded"
	prefKeyFocusedAgentInfo = "sidebar.focused_agent_info.expanded"
	prefKeyTodos            = "sidebar.todos.expanded"
	prefKeyTools            = "sidebar.tools.expanded"
	prefKeySlashCommands    = "sidebar.slash_commands.expanded"
	prefKeyLogs             = "sidebar.logs.expanded"
)
