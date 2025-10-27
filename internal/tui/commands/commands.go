package commands

import (
	"strings"
	"sync"
	"unicode"

	tea "github.com/charmbracelet/bubbletea/v2"
)

// CommandScope indicates how broadly a command should be surfaced.
type CommandScope int

const (
	ScopeBase CommandScope = iota
	ScopeGlobal
	ScopeLocal
)

type Command struct {
	Name             string
	Description      string
	Scope            CommandScope
	RequiresArgument bool
	ArgumentHint     string
	Action           func(Context, string) tea.Cmd
}

// Context exposes methods required by commands to operate on the application.
type Context interface {
	ClearConversation()
	InvokeAgentCommand(agentName, commandName string, args map[string]any) tea.Cmd
	GetCurrentCoreAgentID() string
	ClearFocus()
}

var (
	baseRegistry = []Command{
		{
			Name:        "/agent",
			Description: "choose a managed sub-agent to focus",
			Scope:       ScopeBase,
			Action: func(Context, string) tea.Cmd {
				return nil
			},
		},
		{
			Name:        "/focus",
			Description: "focus on an agent in Builder mode",
			Scope:       ScopeBase,
			Action: func(Context, string) tea.Cmd {
				return nil
			},
		},
		{
			Name:        "/clear",
			Description: "delete all messages in the current conversation",
			Scope:       ScopeBase,
			Action: func(ctx Context, _ string) tea.Cmd {
				ctx.ClearConversation()
				// If in builder mode, also clear focus
				if ctx.GetCurrentCoreAgentID() == "builder" {
					ctx.ClearFocus()
				}
				return nil
			},
		},
	}

	dynamicMu      sync.RWMutex
	globalRegistry []Command
	localRegistry  []Command
)

// SetLocal replaces the agent-scoped command list.
func SetLocal(cmds []Command) {
	dynamicMu.Lock()
	defer dynamicMu.Unlock()
	localRegistry = cloneWithScope(cmds, ScopeLocal)
}

// SetGlobal replaces the globally visible command list.
func SetGlobal(cmds []Command) {
	dynamicMu.Lock()
	defer dynamicMu.Unlock()
	globalRegistry = cloneWithScope(cmds, ScopeGlobal)
}

func cloneWithScope(cmds []Command, scope CommandScope) []Command {
	if len(cmds) == 0 {
		return nil
	}
	out := make([]Command, len(cmds))
	for i, c := range cmds {
		c.Scope = scope
		out[i] = c
	}
	return out
}

func List() []Command {
	dynamicMu.RLock()
	defer dynamicMu.RUnlock()
	total := len(baseRegistry) + len(globalRegistry) + len(localRegistry)
	cmds := make([]Command, 0, total)
	cmds = append(cmds, baseRegistry...)
	cmds = append(cmds, globalRegistry...)
	cmds = append(cmds, localRegistry...)
	return cmds
}

// was executed and the resulting tea.Cmd (if any).
func Execute(name string, ctx Context) (bool, tea.Cmd) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false, nil
	}
	for _, c := range List() {
		if !strings.HasPrefix(trimmed, c.Name) {
			continue
		}
		rest := strings.TrimSpace(trimmed[len(c.Name):])
		if len(trimmed) > len(c.Name) {
			delim := trimmed[len(c.Name)]
			if !unicode.IsSpace(rune(delim)) {
				continue
			}
		}
		if c.Action == nil {
			return true, nil
		}
		return true, c.Action(ctx, rest)
	}
	return false, nil
}
