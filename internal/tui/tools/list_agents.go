package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"opperator/config"
)

//go:embed list_agents.md
var listAgentsDescription []byte

const (
	ListAgentsToolName = "list_agents"
	listAgentsDelay    = 1 * time.Millisecond
)

type ListAgentsParams struct {
	Status string `json:"status"`
}

type ListAgentsMetadata struct {
	Filter       string            `json:"filter"`
	Status       string            `json:"status,omitempty"`
	Count        int               `json:"count"`
	Agents       []string          `json:"agents"`
	Retrieved    string            `json:"retrieved"`
	Descriptions map[string]string `json:"descriptions,omitempty"`
	Processes    []agentProcess    `json:"processes,omitempty"`
}

func ListAgentsSpec() Spec {
	return Spec{
		Name:        ListAgentsToolName,
		Description: strings.TrimSpace(string(listAgentsDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status": map[string]any{
					"type":        "string",
					"enum":        []string{"running", "stopped", "crashed", "all"},
					"description": "Optional status filter",
				},
			},
		},
	}
}

func RunListAgents(ctx context.Context, arguments string) (string, string) {
	if err := sleepWithCancel(ctx, listAgentsDelay); err != nil {
		return "canceled", ""
	}

	var params ListAgentsParams
	_ = json.Unmarshal([]byte(arguments), &params)

	// Load daemon registry to query all enabled daemons
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return fmt.Sprintf("error loading daemon registry: %v", err), ""
	}

	var allFiltered []agentProcess
	displayStatus := strings.TrimSpace(params.Status)
	want := strings.ToLower(displayStatus)

	// Query each enabled daemon
	for _, daemon := range registry.Daemons {
		if !daemon.Enabled {
			continue
		}

		respb, err := ipcRequestToDaemon(ctx, daemon.Name, struct {
			Type string `json:"type"`
		}{Type: "list"})
		if err != nil {
			// Skip unreachable daemons
			continue
		}

		var resp struct {
			Success   bool   `json:"success"`
			Error     string `json:"error"`
			Processes []struct {
				Name         string `json:"name"`
				Description  string `json:"description"`
				Status       string `json:"status"`
				PID          int    `json:"pid"`
				SystemPrompt string `json:"system_prompt"`
			} `json:"processes"`
		}
		if err := json.Unmarshal(respb, &resp); err != nil {
			continue
		}
		if !resp.Success {
			continue
		}

		// Filter and add agents from this daemon
		for _, p := range resp.Processes {
			if want == "" || want == "all" || strings.ToLower(p.Status) == want {
				proc := agentProcess{
					Name:   p.Name,
					Status: p.Status,
					PID:    p.PID,
					Daemon: daemon.Name,
				}
				if desc := strings.TrimSpace(p.Description); desc != "" {
					proc.Description = desc
				}
				allFiltered = append(allFiltered, proc)
			}
		}
	}

	// Sort by daemon first, then by agent name
	sort.Slice(allFiltered, func(i, j int) bool {
		if allFiltered[i].Daemon != allFiltered[j].Daemon {
			return allFiltered[i].Daemon < allFiltered[j].Daemon
		}
		if allFiltered[i].Name == allFiltered[j].Name {
			return allFiltered[i].PID < allFiltered[j].PID
		}
		return allFiltered[i].Name < allFiltered[j].Name
	})

	meta := buildListAgentsMetadata(allFiltered, want, displayStatus)
	mb, _ := json.Marshal(meta)

	if len(allFiltered) == 0 {
		return "", string(mb)
	}

	var lines []string
	for _, p := range allFiltered {
		var parts []string

		// Add daemon indicator for non-local daemons
		if p.Daemon != "" && p.Daemon != "local" {
			parts = append(parts, fmt.Sprintf("@%s", p.Daemon))
		}

		status := strings.TrimSpace(p.Status)
		if status != "" {
			parts = append(parts, status)
		}
		if p.PID > 0 {
			parts = append(parts, fmt.Sprintf("pid=%d", p.PID))
		}
		line := p.Name
		if len(parts) > 0 {
			line += " (" + strings.Join(parts, ", ") + ")"
		}
		if desc := strings.TrimSpace(p.Description); desc != "" {
			line += " — " + desc
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n"), string(mb)
}

func buildListAgentsMetadata(filtered []agentProcess, filter, statusLabel string) ListAgentsMetadata {
	agentNames := make([]string, 0, len(filtered))
	descriptions := make(map[string]string, len(filtered))
	for _, p := range filtered {
		agentNames = append(agentNames, p.Name)
		if desc := strings.TrimSpace(p.Description); desc != "" {
			descriptions[p.Name] = desc
		}
	}
	if len(descriptions) == 0 {
		descriptions = nil
	}
	return ListAgentsMetadata{
		Filter:       filter,
		Status:       statusLabel,
		Count:        len(filtered),
		Agents:       agentNames,
		Retrieved:    time.Now().Format(time.RFC3339),
		Descriptions: descriptions,
		Processes:    filtered,
	}
}
