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
	"github.com/google/uuid"

	"tui/commands"
	"tui/coreagent"
	"tui/internal/conversation"
	"tui/internal/opper"
	"tui/internal/protocol"
	llm "tui/llm"
	"tui/sessionstate"
	tooling "tui/tools"
	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
	"tui/util"
)

type agentController struct {
	convStore *conversation.Store

	coreID     string
	coreName   string
	corePrompt string
	coreColor  string
	coreTools  []tooling.Spec

	activeName          string
	activeDescription   string
	activePrompt        string
	activePromptReplace bool
	activeCommands      []protocol.CommandDescriptor
	activeColor         string

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

func (c *agentController) activeAgentPromptReplace() bool { return c.activePromptReplace }

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
	c.activePromptReplace = false
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
	c.activePromptReplace = meta.SystemPromptReplace
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

func (c *agentController) updateActiveAgentMetadata(agentName, description, prompt, color string, promptReplace bool) bool {
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
	if c.activePromptReplace != promptReplace {
		c.activePromptReplace = promptReplace
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

		// Find which daemon has this agent
		daemonName, err := tooling.FindAgentDaemon(ctx, trimmedAgent)
		if err != nil {
			return agentCommandResultMsg{agent: trimmedAgent, command: trimmedCommand, err: err}
		}

		// Send command to the correct daemon
		data, err := tooling.IPCRequestToDaemon(ctx, daemonName, payload)
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
		agentCopy := trimmedAgent
		commandCopy := commandName
		argumentSchema := cmd.Arguments // Capture the argument schema
		isAsync := cmd.Async            // Capture async flag

		command := commands.Command{
			Name:             slash,
			Description:      description,
			RequiresArgument: requiresArg,
			ArgumentHint:     hint,
			Action: func(ctx commands.Context, argument string) tea.Cmd {
				trimmed := strings.TrimSpace(argument)

				// Commands with argument schema use LLM-powered parser
				if len(argumentSchema) > 0 {
					return parseSlashCommandArgumentsCmd(agentCopy, commandCopy, trimmed, argumentSchema, isAsync)
				}

				// Commands with no arguments - invoke directly
				return ctx.InvokeAgentCommand(agentCopy, commandCopy, nil)
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

	// Set optimistic UI state immediately for instant feedback
	c.setActiveAgentPending(trimmed)
	c.refreshHeader()
	c.refreshSidebar()

	// Fetch full metadata asynchronously (non-blocking)
	return c.fetchAgentMetadataCmd(trimmed)
}

// fetchAgentMetadataCmd fetches agent metadata asynchronously
func (c *agentController) fetchAgentMetadataCmd(agentName string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		meta, err := llm.FetchAgentMetadata(ctx, agentName)
		return agentMetadataFetchedMsg{
			agentName: agentName,
			metadata:  meta,
			err:       err,
		}
	}
}

// setActiveAgentPending sets optimistic UI state while metadata is being fetched
func (c *agentController) setActiveAgentPending(agentName string) {
	c.activeName = agentName
	c.activeDescription = "Loading..."
	c.activePrompt = ""
	c.activeCommands = nil
	c.activeColor = ""
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
		callID := msg.callID
		summary := fmt.Sprintf("Command %s on %s failed: %v", command, agent, msg.err)

		// If we have a callID from slash command parser, finish the tool call with the error
		if callID != "" {
			// Determine if this is an async command
			toolName := "agent_command"
			if desc, ok := m.agents.commandDescriptor(agent, command); ok && desc.Async {
				toolName = tooling.AsyncToolName
			}

			errorCall := tooltypes.Call{
				ID:       callID,
				Name:     toolName,
				Finished: true,
				Input: fmt.Sprintf(`{"agent":"%s","command":"%s","command_failed":true}`,
					strings.ReplaceAll(agent, `"`, `\"`),
					strings.ReplaceAll(command, `"`, `\"`)),
			}
			m.messages.EnsureToolCall(errorCall)

			// Create metadata to indicate failure
			metadata := struct {
				Agent   string `json:"agent"`
				Command string `json:"command"`
				Success bool   `json:"success"`
				Error   string `json:"error"`
			}{
				Agent:   agent,
				Command: command,
				Success: false,
				Error:   msg.err.Error(),
			}
			metadataJSON, _ := json.Marshal(metadata)

			result := tooltypes.Result{
				ToolCallID: callID,
				Name:       toolName,
				Content:    summary,
				Metadata:   string(metadataJSON),
				IsError:    true,
				Pending:    false,
			}
			m.messages.FinishTool(callID, result)
			m.recordToolResultsForSession(m.sessionID, []tooltypes.Result{result})
		}

		return util.ReportError(fmt.Errorf("%s", summary))
	}

	callID := msg.callID

	// Only add user message and tool call if they weren't already added by slash command parser
	if callID == "" {
		// Add user message showing the command that was executed
		prettifiedCommand := toolregistry.PrettifyName(command)
		userMsg := fmt.Sprintf("*%s*", prettifiedCommand)
		m.messages.AddUser(userMsg)
		m.addUserHistory(userMsg)

		// Create tool call for the command
		callID = uuid.New().String()
		call := tooltypes.Call{
			ID:       callID,
			Name:     "agent_command",
			Finished: false,
			Input: fmt.Sprintf(`{"agent":"%s","command":"%s"}`,
				strings.ReplaceAll(agent, `"`, `\"`),
				strings.ReplaceAll(command, `"`, `\"`)),
		}

		// Record tool calls to storage
		m.recordAssistantToolCallsForSession(m.sessionID, []tooltypes.Call{call}, "")

		// First, add/replace the tool call
		m.messages.AddOrReplaceToolCall(call)
	}

	// Then finish it with the result (unless it's async, in which case the async task watcher will handle it)
	// Determine if this is an async command
	isAsync := false
	if desc, ok := m.agents.commandDescriptor(agent, command); ok && desc.Async {
		isAsync = true
	}

	// For async commands, keep the generated tool name so the renderer shows the correct label
	// For sync commands, use agent_command
	toolName := "agent_command"
	if isAsync {
		// Async commands should have their generated tool name already set in the call
		// Look it up to maintain consistency
		if existingToolName, ok := tooling.LookupAgentCommandToolName(agent, command); ok {
			toolName = existingToolName
		} else {
			toolName = tooling.AsyncToolName // Fallback for direct async tool invocations
		}
	}

	// For async commands, just update the call with the task metadata and let the async watcher handle it
	if isAsync {
		// Parse the metadata to extract async task info
		var meta map[string]any
		if msg.result != nil {
			// map[string]interface{} and map[string]any are identical in Go 1.18+
			if metaMap, ok := msg.result.(map[string]any); ok {
				meta = metaMap
			}
		}

		// Update the tool call with the async task information
		call := tooltypes.Call{
			ID:       callID,
			Name:     toolName,
			Finished: false,
			Input:    msg.output,
		}
		m.messages.EnsureToolCall(call)

		// Store metadata so the async watcher can find and track the task
		// Do NOT call FinishTool - the async watcher will update the result as the task progresses
		result := tooltypes.Result{
			ToolCallID: callID,
			Name:       toolName,
			Content:    msg.output,
			Metadata:   mustMarshalJSON(meta),
			IsError:    false,
			Pending:    true,
		}
		m.recordToolResultsForSession(m.sessionID, []tooltypes.Result{result})

		// Register this task as pending so we can track when it completes
		metadataStr := mustMarshalJSON(meta)
		if taskID, _, _, _ := extractAsyncTaskMetadata(metadataStr); taskID != "" {
			m.pendingAsyncTasks[taskID] = callID

			// Track the task with LLM engine's async tracker for real-time monitoring
			// This ensures the LLM is re-invoked when the task completes, just like agent-invoked tasks
			if m.llmEngine != nil {
				m.llmEngine.TrackAsyncTask(taskID, m.sessionID, callID, toolName)
			}
		}

		return nil
	}

	// For sync commands, finish the tool call immediately
	result := tooltypes.Result{
		ToolCallID: callID,
		Name:       toolName,
		Content:    msg.output,
		IsError:    false,
		Pending:    false,
	}
	m.messages.FinishTool(callID, result)

	// Persist tool results to storage
	m.recordToolResultsForSession(m.sessionID, []tooltypes.Result{result})

	return nil
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

// parseSlashCommandArgumentsCmd returns a tea.Cmd that asynchronously parses slash command
// arguments using the Opper API and returns a slashCommandArgumentParsedMsg.
// This is called immediately, so we show the UI first, then parse in the background.
func parseSlashCommandArgumentsCmd(agent, command, rawInput string, schema []protocol.CommandArgument, isAsync bool) tea.Cmd {
	// Create callID upfront so we can track it
	callID := uuid.New().String()

	// Look up the generated tool name for this command
	toolName, _ := tooling.LookupAgentCommandToolName(agent, command)
	if toolName == "" {
		// Fallback to direct tool name if not found in registry
		toolName = "agent_command"
	}

	// Return a batch: first add the UI elements, then parse in background
	return tea.Batch(
		// First, add user message and pending tool call immediately
		func() tea.Msg {
			return slashCommandArgumentParsedMsg{
				agent:    agent,
				command:  command,
				rawInput: rawInput,
				args:     nil, // Will be filled by parser
				callID:   callID,
				err:      nil, // Not an error, just showing UI
				isAsync:  isAsync,
				toolName: toolName,
			}
		},
		// Then parse asynchronously
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			args, err := opper.ParseCommandArguments(ctx, rawInput, schema)
			return slashCommandArgumentParsedMsg{
				agent:    agent,
				command:  command,
				rawInput: rawInput,
				args:     args,
				callID:   callID,
				err:      err,
				isAsync:  isAsync,
				toolName: toolName,
			}
		},
	)
}

// handleSlashCommandArgumentParsed handles the result of slash command argument parsing.
// This is called twice per command: once with args=nil to show UI immediately,
// and again with parsed args to invoke the command.
func (m *Model) handleSlashCommandArgumentParsed(msg slashCommandArgumentParsedMsg) tea.Cmd {
	agent := strings.TrimSpace(msg.agent)
	if agent == "" {
		agent = m.currentActiveAgentName()
	}
	command := strings.TrimSpace(msg.command)
	if command == "" {
		command = "command"
	}

	// First message: show UI (args is nil)
	if msg.args == nil && msg.err == nil {
		// Add user message showing the command that was executed
		prettifiedCommand := toolregistry.PrettifyName(command)
		var userMsg string
		if trimmed := strings.TrimSpace(msg.rawInput); trimmed != "" {
			userMsg = fmt.Sprintf("*%s:* %s", prettifiedCommand, trimmed)
		} else {
			userMsg = fmt.Sprintf("*%s*", prettifiedCommand)
		}
		m.messages.AddUser(userMsg)
		m.addUserHistory(userMsg)

		// Create pending tool call for the command - use the generated tool name
		call := tooltypes.Call{
			ID:       msg.callID,
			Name:     msg.toolName,
			Finished: false,
			Input:    `{"parsing": true}`, // Indicate we're still parsing
		}

		// Record tool calls to storage
		m.recordAssistantToolCallsForSession(m.sessionID, []tooltypes.Call{call}, "")

		// Add the pending tool call to messages
		m.messages.AddOrReplaceToolCall(call)
		return nil
	}

	// Second message: parsing complete, check for errors
	if msg.err != nil {
		// Parsing failed - finish the tool with error status
		errorCall := tooltypes.Call{
			ID:       msg.callID,
			Name:     msg.toolName,
			Finished: true,
			Input: fmt.Sprintf(`{"agent":"%s","command":"%s","parsing_failed":true}`,
				strings.ReplaceAll(agent, `"`, `\"`),
				strings.ReplaceAll(command, `"`, `\"`)),
		}
		m.messages.EnsureToolCall(errorCall)

		// Create metadata to indicate failure
		metadata := struct {
			Agent   string `json:"agent"`
			Command string `json:"command"`
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}{
			Agent:   agent,
			Command: command,
			Success: false,
			Error:   msg.err.Error(),
		}
		metadataJSON, _ := json.Marshal(metadata)

		// Finish the result
		result := tooltypes.Result{
			ToolCallID: msg.callID,
			Name:       msg.toolName,
			Content:    fmt.Sprintf("Failed to parse arguments: %v", msg.err),
			Metadata:   string(metadataJSON),
			IsError:    true,
			Pending:    false,
		}
		m.messages.FinishTool(msg.callID, result)
		m.recordToolResultsForSession(m.sessionID, []tooltypes.Result{result})
		return util.ReportWarn(fmt.Sprintf("Failed to parse arguments for %s: %v", command, msg.err))
	}

	// Update the tool call with the parsed arguments
	call := tooltypes.Call{
		ID:       msg.callID,
		Name:     msg.toolName,
		Finished: false,
		Input: fmt.Sprintf(`{"agent":"%s","command":"%s","args":%s}`,
			strings.ReplaceAll(agent, `"`, `\"`),
			strings.ReplaceAll(command, `"`, `\"`),
			mustMarshalJSON(msg.args)),
	}

	// Update the tool call in messages (this will update the pending call)
	m.messages.EnsureToolCall(call)

	// Invoke the command via RunAgentCommand with the generated tool name
	// For async commands, this will internally call RunAsyncTool
	// For sync commands, it will do direct IPC
	return func() tea.Msg {
		// Use no timeout for async commands, standard timeout for sync
		// Enrich context with session and call IDs so they're sent to the daemon
		var ctx context.Context
		var cancel context.CancelFunc

		baseCtx := tooling.WithSessionContext(context.Background(), m.sessionID, msg.callID)
		if msg.isAsync {
			ctx = baseCtx
		} else {
			ctx, cancel = context.WithTimeout(baseCtx, 5*time.Second)
			defer cancel()
		}

		// Marshal args to JSON string
		// JSON marshaling of args should not fail for the types we support
		argsJSON, _ := json.Marshal(msg.args)

		// Invoke via RunAgentCommand with the generated tool name
		// RunAgentCommand will detect if it's async and call RunAsyncTool internally
		content, metadata := tooling.RunAgentCommand(ctx, msg.toolName, string(argsJSON), "")

		// Parse result metadata if provided
		// If unmarshaling fails, result remains nil which is acceptable
		var result any
		if metadata != "" {
			_ = json.Unmarshal([]byte(metadata), &result)
		}

		return agentCommandResultMsg{
			agent:   msg.agent,
			command: msg.command,
			output:  content,
			result:  result,
			err:     nil,
			callID:  msg.callID,
		}
	}
}

// invokeAgentCommandWithCallID is similar to invokeAgentCommand but passes the callID
// so that handleAgentCommandResult knows not to create a duplicate tool call.
func invokeAgentCommandWithCallID(ac *agentController, agentName, commandName string, args map[string]any, callID string) tea.Cmd {
	trimmedAgent := strings.TrimSpace(agentName)
	if trimmedAgent == "" {
		trimmedAgent = strings.TrimSpace(ac.activeName)
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
		// Async commands don't use a timeout - they're submitted to the daemon
		// and run independently. Sync commands use a 5-second timeout.
		var ctx context.Context
		var cancel context.CancelFunc

		if desc, ok := ac.commandDescriptor(trimmedAgent, trimmedCommand); ok && desc.Async {
			// Async: no timeout, task runs in daemon
			ctx = context.Background()
		} else {
			// Sync: use timeout for IPC
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
		}

		// Find which daemon has this agent
		daemonName, err := tooling.FindAgentDaemon(ctx, trimmedAgent)
		if err != nil {
			return agentCommandResultMsg{agent: trimmedAgent, command: trimmedCommand, err: err, callID: callID}
		}

		// Send command to the correct daemon
		data, err := tooling.IPCRequestToDaemon(ctx, daemonName, payload)
		if err != nil {
			return agentCommandResultMsg{agent: trimmedAgent, command: trimmedCommand, err: err, callID: callID}
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
			return agentCommandResultMsg{agent: trimmedAgent, command: trimmedCommand, err: fmt.Errorf("decode command response: %w", err), callID: callID}
		}

		if !resp.Success {
			if resp.Error == "" {
				resp.Error = "unknown error"
			}
			return agentCommandResultMsg{agent: trimmedAgent, command: trimmedCommand, err: errors.New(resp.Error), callID: callID}
		}

		if !resp.Command.Success {
			errMsg := resp.Command.Error
			if errMsg == "" {
				errMsg = "command failed"
			}
			return agentCommandResultMsg{agent: trimmedAgent, command: trimmedCommand, err: errors.New(errMsg), callID: callID}
		}

		var pretty string
		if resp.Command.Result != nil {
			formatted, err := json.MarshalIndent(resp.Command.Result, "", "  ")
			if err == nil {
				pretty = string(formatted)
			}
		}

		return agentCommandResultMsg{agent: trimmedAgent, command: trimmedCommand, result: resp.Command.Result, output: pretty, callID: callID}
	}
}

// mustMarshalJSON marshals a value to JSON string, returning "{}" on error.
func mustMarshalJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
