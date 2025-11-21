package publish

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"opperator/internal/agent"
)

// AgentMetadata represents the metadata file (agent.json) published with an agent
type AgentMetadata struct {
	// Core agent configuration (from agents.yaml)
	Name            string            `json:"name"`
	Description     string            `json:"description,omitempty"`
	Command         string            `json:"command"`
	Args            []string          `json:"args,omitempty"`
	ProcessRoot     string            `json:"process_root,omitempty"`
	Env             map[string]string `json:"env,omitempty"`
	AutoRestart     bool              `json:"auto_restart,omitempty"`
	MaxRestarts     int               `json:"max_restarts,omitempty"`
	StartWithDaemon *bool             `json:"start_with_daemon,omitempty"`
	SystemPrompt    string            `json:"system_prompt,omitempty"`

	// Publishing metadata
	RequiredSecrets  []string `json:"required_secrets,omitempty"` // Auto-detected secret names
	Version          string   `json:"version"`                     // Semantic version
	PublishedAt      string   `json:"published_at"`                // ISO 8601 timestamp
	OpperatorVersion string   `json:"opperator_version,omitempty"` // CLI version used to publish
}

// generateAgentMetadata creates an AgentMetadata struct from an agent config
func generateAgentMetadata(config *agent.AgentConfig, detectedSecrets []string, version string) *AgentMetadata {
	return &AgentMetadata{
		Name:             config.Name,
		Description:      config.Description,
		Command:          config.Command,
		Args:             config.Args,
		ProcessRoot:      config.ProcessRoot,
		Env:              config.Env,
		AutoRestart:      config.AutoRestart,
		MaxRestarts:      config.MaxRestarts,
		StartWithDaemon:  config.StartWithDaemon,
		SystemPrompt:     config.SystemPrompt,
		RequiredSecrets:  detectedSecrets,
		Version:          version,
		PublishedAt:      time.Now().UTC().Format(time.RFC3339),
		OpperatorVersion: getOpperatorVersion(),
	}
}

// writeAgentMetadata writes the agent.json file to the specified directory
func writeAgentMetadata(metadata *AgentMetadata, destDir string) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	metadataPath := filepath.Join(destDir, "agent.json")
	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// getOpperatorVersion returns the current opperator CLI version
// TODO: Replace with actual version from build info
func getOpperatorVersion() string {
	return "0.1.0" // Placeholder
}
