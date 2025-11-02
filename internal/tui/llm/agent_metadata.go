package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"opperator/config"
	"tui/components/sidebar"
	"tui/internal/protocol"

	tooling "tui/tools"
)

type AgentMetadata struct {
	Name         string
	Description  string
	SystemPrompt string
	Commands     []protocol.CommandDescriptor
	Color        string
}

type AgentInfo struct {
	Name         string
	Description  string
	SystemPrompt string
	Status       string
	Color        string
	Daemon       string // Which daemon this agent is running on
}

// ListAgents retrieves agents from all enabled daemons in the registry.
func ListAgents(ctx context.Context) ([]AgentInfo, error) {
	// Load daemon registry
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		// Fallback to local daemon only if registry fails
		return listAgentsFromDaemon(ctx, "local")
	}

	var allAgents []AgentInfo

	// Collect agents from all enabled daemons
	for _, daemon := range registry.Daemons {
		if !daemon.Enabled {
			continue
		}

		// Get agents from this daemon
		agents, err := listAgentsFromDaemonConfig(ctx, daemon.Name)
		if err != nil {
			// Log error but continue with other daemons
			// This allows the TUI to work even if some daemons are offline
			continue
		}

		// Tag each agent with its daemon and add to collection
		for i := range agents {
			agents[i].Daemon = daemon.Name
		}
		allAgents = append(allAgents, agents...)
	}

	return allAgents, nil
}

// listAgentsFromDaemon retrieves agents from a specific daemon by name
func listAgentsFromDaemon(ctx context.Context, daemonName string) ([]AgentInfo, error) {
	return listAgentsFromDaemonConfig(ctx, daemonName)
}

// listAgentsFromDaemonConfig is the internal implementation for listing agents
func listAgentsFromDaemonConfig(ctx context.Context, daemonName string) ([]AgentInfo, error) {
	listPayload := struct {
		Type string `json:"type"`
	}{Type: "list"}

	data, err := tooling.IPCRequestToDaemon(ctx, daemonName, listPayload)
	if err != nil {
		return nil, err
	}

	var listResp struct {
		Success   bool   `json:"success"`
		Error     string `json:"error"`
		Processes []struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			SystemPrompt string `json:"system_prompt"`
			Status       string `json:"status"`
			Color        string `json:"color"`
		} `json:"processes"`
	}
	if err := json.Unmarshal(data, &listResp); err != nil {
		return nil, fmt.Errorf("decode list response: %w", err)
	}
	if !listResp.Success {
		if listResp.Error == "" {
			listResp.Error = "unknown error"
		}
		return nil, errors.New(listResp.Error)
	}

	agents := make([]AgentInfo, 0, len(listResp.Processes))
	for _, proc := range listResp.Processes {
		agents = append(agents, AgentInfo{
			Name:         proc.Name,
			Description:  proc.Description,
			SystemPrompt: proc.SystemPrompt,
			Status:       proc.Status,
			Color:        proc.Color,
			// Daemon field will be set by caller
		})
	}
	return agents, nil
}

// FetchAgentMetadata retrieves metadata for the given agent name by asking the
func FetchAgentMetadata(ctx context.Context, name string) (AgentMetadata, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return AgentMetadata{}, fmt.Errorf("agent name required")
	}

	var result AgentMetadata
	var agentDaemon string // Track which daemon has this agent
	agents, err := ListAgents(ctx)
	if err != nil {
		return AgentMetadata{}, err
	}
	for _, proc := range agents {
		if strings.EqualFold(proc.Name, trimmed) {
			result.Name = proc.Name
			result.Description = proc.Description
			result.SystemPrompt = proc.SystemPrompt
			result.Color = proc.Color
			agentDaemon = proc.Daemon // Remember which daemon has this agent
			break
		}
	}
	if result.Name == "" {
		return AgentMetadata{}, fmt.Errorf("agent %s not found", trimmed)
	}

	// Default to local if daemon not specified (backward compatibility)
	if agentDaemon == "" {
		agentDaemon = "local"
	}

	// Try to fetch commands, but don't fail if agent is stopped/crashed
	// The description, system prompt, and color are already set from the config
	cmdPayload := struct {
		Type      string `json:"type"`
		AgentName string `json:"agent_name"`
	}{Type: "list_commands", AgentName: result.Name}

	// Query the correct daemon for commands
	cmdData, err := tooling.IPCRequestToDaemon(ctx, agentDaemon, cmdPayload)
	if err != nil {
		// Agent might be stopped - return metadata without commands
		return result, nil
	}

	var cmdResp struct {
		Success  bool                         `json:"success"`
		Error    string                       `json:"error"`
		Commands []protocol.CommandDescriptor `json:"commands"`
	}
	if err := json.Unmarshal(cmdData, &cmdResp); err != nil {
		// Failed to decode - return metadata without commands
		return result, nil
	}
	if !cmdResp.Success {
		// Command list failed (agent might be stopped) - return metadata without commands
		return result, nil
	}

	result.Commands = protocol.NormalizeCommandDescriptors(cmdResp.Commands)
	return result, nil
}

// FetchAgentLogs retrieves recent logs for the given agent name.
func FetchAgentLogs(ctx context.Context, name string, maxLines int) ([]string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, fmt.Errorf("agent name required")
	}

	if maxLines <= 0 {
		maxLines = 50 // Default to 50 lines
	}

	// Find which daemon has this agent
	agentDaemon := "local" // Default
	agents, err := ListAgents(ctx)
	if err == nil {
		for _, agent := range agents {
			if strings.EqualFold(agent.Name, trimmed) {
				if agent.Daemon != "" {
					agentDaemon = agent.Daemon
				}
				break
			}
		}
	}

	payload := struct {
		Type      string `json:"type"`
		AgentName string `json:"agent_name"`
	}{Type: "get_logs", AgentName: trimmed}

	// Query the correct daemon for logs
	data, err := tooling.IPCRequestToDaemon(ctx, agentDaemon, payload)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Success bool     `json:"success"`
		Error   string   `json:"error"`
		Logs    []string `json:"logs"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode logs response: %w", err)
	}
	if !resp.Success {
		if resp.Error == "" {
			resp.Error = "unknown error"
		}
		return nil, errors.New(resp.Error)
	}

	logs := resp.Logs
	if len(logs) > maxLines {
		logs = logs[len(logs)-maxLines:]
	}

	return logs, nil
}

// FetchAgentCustomSections retrieves custom sidebar sections for the given agent name.
func FetchAgentCustomSections(ctx context.Context, name string) ([]sidebar.CustomSection, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, nil // No agent, no sections
	}

	// Find which daemon has this agent
	agentDaemon := "local" // Default
	agents, err := ListAgents(ctx)
	if err == nil {
		for _, agent := range agents {
			if strings.EqualFold(agent.Name, trimmed) {
				if agent.Daemon != "" {
					agentDaemon = agent.Daemon
				}
				break
			}
		}
	}

	payload := struct {
		Type      string `json:"type"`
		AgentName string `json:"agent_name"`
	}{Type: "get_custom_sections", AgentName: trimmed}

	// Query the correct daemon for custom sections
	data, err := tooling.IPCRequestToDaemon(ctx, agentDaemon, payload)
	if err != nil {
		return nil, nil
	}

	var resp struct {
		Success  bool                      `json:"success"`
		Error    string                    `json:"error"`
		Sections []sidebar.CustomSection   `json:"sections"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, nil // Ignore decode errors for now
	}
	if !resp.Success {
		return nil, nil // Ignore errors for now
	}

	// Trim leading/trailing whitespace from all section fields to prevent alignment issues
	for i := range resp.Sections {
		resp.Sections[i].ID = strings.TrimSpace(resp.Sections[i].ID)
		resp.Sections[i].Title = strings.TrimSpace(resp.Sections[i].Title)
		resp.Sections[i].Content = strings.TrimSpace(resp.Sections[i].Content)
	}

	return resp.Sections, nil
}
