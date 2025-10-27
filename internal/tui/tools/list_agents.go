package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
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

	respb, err := ipcRequestCtx(ctx, struct {
		Type string `json:"type"`
	}{Type: "list"})
	if err != nil {
		return fmt.Sprintf("error: %v", err), ""
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
		return fmt.Sprintf("error decoding response: %v", err), ""
	}
	if !resp.Success {
		if strings.TrimSpace(resp.Error) == "" {
			resp.Error = "unknown error"
		}
		return "error: " + resp.Error, ""
	}

	displayStatus := strings.TrimSpace(params.Status)
	want := strings.ToLower(displayStatus)
	filtered := make([]agentProcess, 0, len(resp.Processes))
	for _, p := range resp.Processes {
		if want == "" || want == "all" || strings.ToLower(p.Status) == want {
			filtered = append(filtered, agentProcess{Name: p.Name, Status: p.Status, PID: p.PID})
			if desc := strings.TrimSpace(p.Description); desc != "" {
				filtered[len(filtered)-1].Description = desc
			}
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Name == filtered[j].Name {
			return filtered[i].PID < filtered[j].PID
		}
		return filtered[i].Name < filtered[j].Name
	})

	meta := buildListAgentsMetadata(filtered, want, displayStatus)
	mb, _ := json.Marshal(meta)

	if len(filtered) == 0 {
		return "", string(mb)
	}

	var lines []string
	for _, p := range filtered {
		var parts []string
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
