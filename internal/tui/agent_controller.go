package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"

	"tui/commands"
	"tui/coreagent"
	"tui/internal/conversation"
	"tui/internal/protocol"
	llm "tui/llm"
	"tui/sessionstate"
	tooling "tui/tools"
	"tui/util"
)

type agentController struct {
	convStore *conversation.Store

	coreID     string
	coreName   string
	corePrompt string
	coreColor  string
	coreTools  []tooling.Spec

	activeName        string
	activeDescription string
	activePrompt      string
	activeCommands    []protocol.CommandDescriptor
	activeColor       string

	// Focused agent tracking for Builder command inheritance
	focusedAgentName     string
	focusedAgentCommands []protocol.CommandDescriptor

	sessionID string

	refreshHeader        func()
	refreshHelp          func()
	refreshConversations func()
	refreshSidebar       func() tea.Cmd
	setInputAgentID      func(string)
}

func newAgentController(
	convStore *conversation.Store,
	refreshHeader func(),
	refreshHelp func(),
	refreshConversations func(),
	refreshSidebar func() tea.Cmd,
	setInputAgentID func(string),
) *agentController {
	ctrl := &agentController{
		convStore:            convStore,
		refreshHeader:        noOp(refreshHeader),
		refreshHelp:          noOp(refreshHelp),
		refreshConversations: noOp(refreshConversations),
		refreshSidebar:       noOpCmd(refreshSidebar),
		setInputAgentID:      noOpStr(setInputAgentID),
	}
	ctrl.resetActiveAgentState()
	return ctrl
}

func noOp(fn func()) func() {
	if fn == nil {
		return func() {}
	}
	return fn
}

func noOpStr(fn func(string)) func(string) {
	if fn == nil {
		return func(string) {}
	}
	return fn
}

func noOpCmd(fn func() tea.Cmd) func() tea.Cmd {
	if fn == nil {
		return func() tea.Cmd { return nil }
	}
	return fn
}

func (c *agentController) setSessionID(id string) {
	c.sessionID = id
}

func (c *agentController) coreAgentID() string { return c.coreID }

func (c *agentController) coreAgentName() string { return c.coreName }

func (c *agentController) coreAgentPrompt() string { return c.corePrompt }

func (c *agentController) coreAgentColor() string { return c.coreColor }

func (c *agentController) activeAgentColor() string { return c.activeColor }

func (c *agentController) coreAgentToolsCopy() []tooling.Spec {
	if len(c.coreTools) == 0 {
		return nil
	}
	out := make([]tooling.Spec, len(c.coreTools))
	copy(out, c.coreTools)
	return out
}

func (c *agentController) activeAgentName() string { return c.activeName }

func (c *agentController) activeAgentDescription() string { return c.activeDescription }

func (c *agentController) activeAgentPrompt() string { return c.activePrompt }

func (c *agentController) activeAgentCommandsCopy() []protocol.CommandDescriptor {
	if len(c.activeCommands) == 0 {
		return nil
	}
	out := make([]protocol.CommandDescriptor, len(c.activeCommands))
	copy(out, c.activeCommands)
	return out
}

func (c *agentController) resetActiveAgentState() {
	c.activeName = ""
	c.activeDescription = ""
	c.activePrompt = ""
	c.activeCommands = nil
	c.activeColor = ""
	commands.SetLocal(nil)
	commands.SetGlobal(nil)
}

func (c *agentController) setFocusedAgent(agentName string) {
	c.focusedAgentName = strings.TrimSpace(agentName)
	c.focusedAgentCommands = nil

	// If there's a focused agent, try to fetch its commands
	if c.focusedAgentName != "" {
		if meta, err := llm.FetchAgentMetadata(context.Background(), c.focusedAgentName); err == nil {
			c.focusedAgentCommands = append([]protocol.CommandDescriptor(nil), meta.Commands...)
		}
	}
}

func (c *agentController) clearFocusedAgent() {
	c.focusedAgentName = ""
	c.focusedAgentCommands = nil
}

func (c *agentController) focusedAgent() string {
	return c.focusedAgentName
}

func (c *agentController) applyCoreAgentDefinition(def coreagent.Definition) {
	c.coreID = def.ID
	c.coreName = def.Name
	c.corePrompt = def.Prompt
	c.coreColor = def.Color
	c.coreTools = append([]tooling.Spec(nil), def.Tools...)
	c.setInputAgentID(def.ID)
	c.refreshHeader()
	c.refreshHelp()
	_ = c.refreshSidebar()
}

func (c *agentController) findCoreAgentDefinition(id string) (coreagent.Definition, bool) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		def := coreagent.Default()
		return def, true
	}
	if def, ok := coreagent.Lookup(strings.ToLower(trimmed)); ok {
		return def, true
	}
	for _, def := range coreagent.All() {
		if strings.EqualFold(def.Name, trimmed) || strings.EqualFold(def.ID, trimmed) {
			return def, true
		}
	}
	return coreagent.Definition{}, false
}

func (c *agentController) setCoreAgent(id string) {
	def, ok := c.findCoreAgentDefinition(id)
	if !ok {
		def = coreagent.Default()
	}
	if strings.EqualFold(strings.TrimSpace(c.coreID), def.ID) {
		return
	}
	c.applyCoreAgentDefinition(def)
}

func (c *agentController) switchCoreAgent(id string) tea.Cmd {
	def, ok := c.findCoreAgentDefinition(id)
	if !ok {
		return util.ReportError(fmt.Errorf("unknown core agent %q", strings.TrimSpace(id)))
	}
	if strings.TrimSpace(c.activeName) != "" {
		c.clearActiveAgent()
	}
	sameCore := strings.EqualFold(strings.TrimSpace(c.coreID), def.ID)
	if sameCore {
		return nil
	}
	c.applyCoreAgentDefinition(def)
	c.persistCoreSelection(def.ID)
	return nil
}

func (c *agentController) baseToolSpecs() []tooling.Spec {
	if strings.TrimSpace(c.activeName) != "" {
		specs := tooling.OpperatorSpecs()
		if len(specs) == 0 {
			return nil
		}
		return append([]tooling.Spec(nil), specs...)
	}
	if len(c.coreTools) == 0 {
		return nil
	}
	return append([]tooling.Spec(nil), c.coreTools...)
}

func (c *agentController) clearActiveAgent() {
	// Notify agent of deactivation before clearing
	if oldName := strings.TrimSpace(c.activeName); oldName != "" {
		tooling.SendLifecycleEvent(oldName, "agent_deactivated", map[string]interface{}{
			"next_agent": nil,
		})
	}

	c.resetActiveAgentState()
	if c.convStore != nil && strings.TrimSpace(c.sessionID) != "" {
		_ = c.convStore.UpdateActiveAgent(context.Background(), c.sessionID, "")
	}
	c.refreshConversations()
	c.refreshHeader()
	c.refreshHelp()
	_ = c.refreshSidebar()
}

func (c *agentController) applyActiveAgent(meta llm.AgentMetadata, persist bool) {
	previousAgent := strings.TrimSpace(c.activeName)

	c.resetActiveAgentState()
	c.activeName = meta.Name
	c.activeDescription = meta.Description
	c.activePrompt = meta.SystemPrompt
	c.activeCommands = append([]protocol.CommandDescriptor(nil), meta.Commands...)
	c.activeColor = meta.Color
	tooling.BuildAgentCommandTools(meta.Name, c.activeCommands)
	localCmds, globalCmds := c.buildSlashCommandsForAgent(meta.Name, c.activeCommands)
	commands.SetGlobal(globalCmds)
	commands.SetLocal(localCmds)
	if persist && c.convStore != nil && strings.TrimSpace(c.sessionID) != "" {
		_ = c.convStore.UpdateActiveAgent(context.Background(), c.sessionID, meta.Name)
		c.refreshConversations()
	}
	c.refreshHeader()
	c.refreshHelp()
	c.refreshSidebar()

	// Notify new agent of activation
	tooling.SendLifecycleEvent(meta.Name, "agent_activated", map[string]interface{}{
		"previous_agent":  previousAgent,
		"conversation_id": c.sessionID,
	})
}

func (c *agentController) updateActiveAgentCommands(agentName string, cmds []protocol.CommandDescriptor) bool {
	trimmed := strings.TrimSpace(agentName)
	if trimmed == "" {
		return false
	}
	if !strings.EqualFold(trimmed, strings.TrimSpace(c.activeName)) {
		return false
	}

	normalized := protocol.NormalizeCommandDescriptors(cmds)
	if commandsEqual(normalized, c.activeCommands) {
		return false
	}

	c.activeCommands = append([]protocol.CommandDescriptor(nil), normalized...)
	tooling.BuildAgentCommandTools(c.activeName, c.activeCommands)
	localCmds, globalCmds := c.buildSlashCommandsForAgent(c.activeName, c.activeCommands)
	commands.SetGlobal(globalCmds)
	commands.SetLocal(localCmds)
	c.refreshHelp()
	c.refreshSidebar()
	return true
}

func (c *agentController) updateActiveAgentMetadata(agentName, description, prompt, color string) bool {
	trimmed := strings.TrimSpace(agentName)
	if trimmed == "" || !strings.EqualFold(trimmed, strings.TrimSpace(c.activeName)) {
		return false
	}

	desc := strings.TrimSpace(description)
	metaPrompt := strings.TrimSpace(prompt)
	newColor := strings.TrimSpace(color)

	changed := false
	if c.activeDescription != desc {
		c.activeDescription = desc
		changed = true
	}
	if c.activePrompt != metaPrompt {
		c.activePrompt = metaPrompt
		changed = true
	}
	if newColor != "" && c.activeColor != newColor {
		c.activeColor = newColor
		changed = true
	}

	if changed {
		c.refreshHeader()
		c.refreshSidebar()
	}
	return changed
}

func (c *agentController) invokeAgentCommand(agentName, commandName string, args map[string]any) tea.Cmd {
	trimmedAgent := strings.TrimSpace(agentName)
	if trimmedAgent == "" {
		trimmedAgent = strings.TrimSpace(c.activeName)
	}
	if trimmedAgent == "" {
		return util.ReportWarn("No active agent selected")
	}

	trimmedCommand := strings.TrimSpace(commandName)
	if trimmedCommand == "" {
		return util.ReportWarn("Command name required")
	}

	payload := struct {
		Type      string         `json:"type"`
		AgentName string         `json:"agent_name"`
		Command   string         `json:"command"`
		Args      map[string]any `json:"args,omitempty"`
	}{
		Type:      "command",
		AgentName: trimmedAgent,
		Command:   trimmedCommand,
		Args:      args,
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		data, err := tooling.IPCRequestCtx(ctx, payload)
		if err != nil {
			return agentCommandResultMsg{agent: trimmedAgent, command: trimmedCommand, err: err}
		}

		var resp struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
			Command struct {
				Success bool   `json:"success"`
				Error   string `json:"error"`
				Result  any    `json:"result"`
			} `json:"command"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return agentCommandResultMsg{agent: trimmedAgent, command: trimmedCommand, err: fmt.Errorf("decode command response: %w", err)}
		}

		if !resp.Success {
			if resp.Error == "" {
				resp.Error = "unknown error"
			}
			return agentCommandResultMsg{agent: trimmedAgent, command: trimmedCommand, err: errors.New(resp.Error)}
		}

		if !resp.Command.Success {
			errMsg := resp.Command.Error
			if errMsg == "" {
				errMsg = "command failed"
			}
			return agentCommandResultMsg{agent: trimmedAgent, command: trimmedCommand, err: errors.New(errMsg)}
		}

		var pretty string
		if resp.Command.Result != nil {
			formatted, err := json.MarshalIndent(resp.Command.Result, "", "  ")
			if err == nil {
				pretty = string(formatted)
			}
		}

		return agentCommandResultMsg{agent: trimmedAgent, command: trimmedCommand, result: resp.Command.Result, output: pretty}
	}
}

func (c *agentController) buildSlashCommandsForAgent(agentName string, cmds []protocol.CommandDescriptor) ([]commands.Command, []commands.Command) {
	trimmedAgent := strings.TrimSpace(agentName)
	if trimmedAgent == "" || len(cmds) == 0 {
		return nil, nil
	}

	local := make([]commands.Command, 0, len(cmds))
	global := make([]commands.Command, 0, len(cmds))

	for _, cmd := range cmds {
		if !hasSlashExposure(cmd) {
			continue
		}

		slash := strings.TrimSpace(cmd.SlashCommand)
		if slash == "" {
			continue
		}

		commandName := strings.TrimSpace(cmd.Name)
		if commandName == "" {
			continue
		}

		title := strings.TrimSpace(cmd.Title)
		if title == "" {
			title = commandName
		}

		description := strings.TrimSpace(cmd.Description)
		if description == "" {
			description = fmt.Sprintf("Invoke %s on sub-agent %s", title, trimmedAgent)
		}

		requiresArg := cmd.ArgumentRequired
		hint := strings.TrimSpace(cmd.ArgumentHint)
		slashCopy := slash
		titleCopy := title
		agentCopy := trimmedAgent
		commandCopy := commandName

		command := commands.Command{
			Name:             slash,
			Description:      description,
			RequiresArgument: requiresArg,
			ArgumentHint:     hint,
			Action: func(ctx commands.Context, argument string) tea.Cmd {
				trimmed := strings.TrimSpace(argument)
				if requiresArg && trimmed == "" {
					label := titleCopy
					if label == "" {
						label = commandCopy
					}
					return util.ReportWarn(fmt.Sprintf("%s requires an argument", slashCopy))
				}
				var args map[string]any
				if trimmed != "" {
					args = map[string]any{"input": trimmed}
				}
				return ctx.InvokeAgentCommand(agentCopy, commandCopy, args)
			},
		}

		if cmd.SlashScope == protocol.SlashCommandScopeGlobal {
			global = append(global, command)
		} else {
			local = append(local, command)
		}
	}

	return local, global
}

func (c *agentController) handleAgentCommand(target string) tea.Cmd {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return nil
	}
	if strings.EqualFold(trimmed, "none") || strings.EqualFold(trimmed, "clear") || strings.EqualFold(trimmed, "default") {
		if c.activeName == "" {
			return nil
		}
		c.clearActiveAgent()
		return nil
	}

	if def, ok := c.findCoreAgentDefinition(trimmed); ok {
		return c.switchCoreAgent(def.ID)
	}

	meta, err := llm.FetchAgentMetadata(context.Background(), trimmed)
	if err != nil {
		return util.ReportError(fmt.Errorf("fetch agent %s: %w", trimmed, err))
	}
	c.applyActiveAgent(meta, true)
	if len(meta.Commands) == 0 {
		return util.ReportWarn(fmt.Sprintf("Agent %s exposes no commands.", meta.Name))
	}
	return nil
}

func (c *agentController) restoreActiveAgentForSession(sessionID string) []tea.Cmd {
	var alerts []tea.Cmd
	refreshed := false
	if c.convStore != nil {
		if conv, err := c.convStore.Get(context.Background(), sessionID); err == nil {
			if agent := strings.TrimSpace(conv.ActiveAgent); agent != "" {
				if strings.EqualFold(agent, coreagent.IDBuilder) {
					if !strings.EqualFold(strings.TrimSpace(c.coreID), coreagent.IDBuilder) {
						c.setCoreAgent(coreagent.IDBuilder)
						refreshed = true
					}
				} else if meta, err := llm.FetchAgentMetadata(context.Background(), agent); err == nil {
					c.applyActiveAgent(meta, false)
					refreshed = true
				} else {
					c.activeName = agent
					alerts = append(alerts, util.ReportWarn(fmt.Sprintf("Agent %s unavailable: %v", agent, err)))
				}
			}
		}
	}
	if !refreshed {
		c.refreshHeader()
		c.refreshHelp()
		c.refreshSidebar()
	}
	return alerts
}

func (c *agentController) persistCoreSelection(id string) {
	if c.convStore == nil || strings.TrimSpace(c.sessionID) == "" {
		return
	}
	value := ""
	if strings.EqualFold(strings.TrimSpace(id), coreagent.IDBuilder) {
		value = coreagent.IDBuilder
	}
	if err := c.convStore.UpdateActiveAgent(context.Background(), c.sessionID, value); err == nil {
		c.refreshConversations()
	}
}

func (c *agentController) collectSessionAgentOptions() ([]sessionstate.AgentOption, error) {
	agents, err := llm.ListAgents(context.Background())
	if err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return nil, nil
	}
	options := make([]sessionstate.AgentOption, 0, len(agents))
	seen := make(map[string]struct{})
	for _, agent := range agents {
		name := strings.TrimSpace(agent.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		options = append(options, sessionstate.AgentOption{
			Name:        name,
			Status:      strings.TrimSpace(agent.Status),
			Description: strings.TrimSpace(agent.Description),
		})
	}
	sort.SliceStable(options, func(i, j int) bool {
		return strings.ToLower(options[i].Name) < strings.ToLower(options[j].Name)
	})
	return options, nil
}

func (c *agentController) baseSpecsForSession() []tooling.Spec {
	return c.baseToolSpecs()
}

func (c *agentController) commandDescriptor(agentName, commandName string) (protocol.CommandDescriptor, bool) {
	trimmedCommand := strings.TrimSpace(commandName)
	if trimmedCommand == "" {
		return protocol.CommandDescriptor{}, false
	}
	targetAgent := strings.TrimSpace(agentName)
	if targetAgent == "" {
		targetAgent = c.activeName
	}
	if !strings.EqualFold(targetAgent, c.activeName) {
		return protocol.CommandDescriptor{}, false
	}
	for _, cmd := range c.activeCommands {
		if strings.EqualFold(cmd.Name, trimmedCommand) {
			return cmd, true
		}
	}
	return protocol.CommandDescriptor{}, false
}

func (c *agentController) extraSpecsForSession() []tooling.Spec {
	// If Builder is active and there's a focused agent, return the focused agent's commands
	if strings.TrimSpace(c.activeName) == "" && strings.EqualFold(strings.TrimSpace(c.coreID), coreagent.IDBuilder) {
		if c.focusedAgentName != "" && len(c.focusedAgentCommands) > 0 {
			return tooling.BuildAgentCommandTools(c.focusedAgentName, c.focusedAgentCommands)
		}
		return nil
	}

	// Otherwise use the normal logic (for active agents)
	if strings.TrimSpace(c.activeName) == "" || len(c.activeCommands) == 0 {
		return nil
	}
	return tooling.BuildAgentCommandTools(c.activeName, c.activeCommands)
}

func (c *agentController) coreAgentMetadata() (string, string, string, []tooling.Spec) {
	return c.coreName, c.corePrompt, c.coreColor, c.coreAgentToolsCopy()
}

func commandsEqual(a, b []protocol.CommandDescriptor) bool {
	return reflect.DeepEqual(a, b)
}

func (c *agentController) activeAgentMetadata() (string, string, []protocol.CommandDescriptor) {
	return c.activeName, c.activePrompt, c.activeAgentCommandsCopy()
}

func (c *agentController) handleCycleAgentResult(msg cycleAgentResultMsg) tea.Cmd {
	if msg.clearActive && strings.TrimSpace(c.activeName) != "" {
		c.clearActiveAgent()
	}
	if msg.err != nil {
		return util.ReportError(msg.err)
	}
	var cmds []tea.Cmd
	if msg.coreID != "" {
		if cmd := c.switchCoreAgent(msg.coreID); cmd != nil {
			cmds = append(cmds, cmd)
		}
	} else if msg.hasMeta {
		c.applyActiveAgent(msg.meta, true)
		if msg.warn != "" {
			cmds = append(cmds, util.ReportWarn(msg.warn))
		}
	} else if msg.warn != "" {
		cmds = append(cmds, util.ReportWarn(msg.warn))
	}
	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	default:
		return tea.Batch(cmds...)
	}
}

// Model wrapper methods for agent commands
// These methods are part of Model but organized here with agent controller logic

func (m *Model) InvokeAgentCommand(agentName, commandName string, args map[string]any) tea.Cmd {
	if m.agents == nil {
		return util.ReportWarn("Agent support unavailable")
	}
	if desc, ok := m.agents.commandDescriptor(agentName, commandName); ok && desc.Async {
		return m.scheduleAsyncAgentCommand(agentName, desc, args)
	}
	return m.agents.invokeAgentCommand(agentName, commandName, args)
}

func (m *Model) handleAgentCommandResult(msg agentCommandResultMsg) tea.Cmd {
	agent := strings.TrimSpace(msg.agent)
	if agent == "" {
		agent = m.currentActiveAgentName()
	}
	command := strings.TrimSpace(msg.command)
	if command == "" {
		command = "command"
	}

	if msg.err != nil {
		summary := fmt.Sprintf("Command %s on %s failed: %v", command, agent, msg.err)
		return util.ReportError(fmt.Errorf("%s", summary))
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Command %s on %s completed.", command, agent))
	if output := strings.TrimSpace(msg.output); output != "" {
		builder.WriteString("\n\n")
		builder.WriteString(output)
	}
	content := builder.String()

	m.messages.AddAssistantStart(llm.ModelName())
	m.messages.AppendAssistant(content)
	m.messages.EndAssistant()
	m.addAssistantContentHistory(m.sessionID, content)

	return util.ReportInfo(fmt.Sprintf("Command %s on %s completed", command, agent))
}

func (m *Model) handleAgentCommand(input string) tea.Cmd {
	fields := strings.Fields(input)
	if len(fields) <= 1 {
		m.input.SetValue("/agent ")
		return m.ensureAgentPicker("")
	}
	target := fields[1]
	m.agentPicker = nil
	m.input.SetValue("")
	if m.agents == nil {
		return util.ReportWarn("Agent support unavailable")
	}
	return m.agents.handleAgentCommand(target)
}

func (m *Model) handleFocusCommand(input string) tea.Cmd {
	// Only available in Builder mode
	if m.currentCoreAgentID() != coreagent.IDBuilder {
		return util.ReportWarn("/focus command is only available in Builder mode")
	}

	fields := strings.Fields(input)
	if len(fields) <= 1 {
		m.input.SetValue("/focus ")
		return m.ensureFocusAgentPicker("")
	}
	target := fields[1]
	m.agentPicker = nil
	m.input.SetValue("")

	// Call RunFocusAgent directly - this will publish the event through the pubsub system
	// which will be received by handleFocusAgentEvent
	go func() {
		ctx := context.Background()
		args := fmt.Sprintf(`{"agent_name": %q}`, target)
		tooling.RunFocusAgent(ctx, args)
	}()

	return nil
}
