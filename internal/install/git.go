package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// PackageRef represents a parsed agent package reference
type PackageRef struct {
	User    string // GitHub username
	Repo    string // Repository name
	RepoURL string // Full clone URL
}

// AgentMetadata represents the metadata from agent.json
// This mirrors the struct from internal/publish/metadata.go
type AgentMetadata struct {
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
	RequiredSecrets []string          `json:"required_secrets,omitempty"`
	Version         string            `json:"version"`
	PublishedAt     string            `json:"published_at"`
	OpperatorVersion string           `json:"opperator_version,omitempty"`
}

// parsePackageRef parses various package reference formats into a PackageRef
// Supported formats:
//   - username/repo
//   - https://github.com/username/repo
//   - github.com/username/repo
func parsePackageRef(ref string) (*PackageRef, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("package reference cannot be empty")
	}

	// Remove .git suffix if present
	ref = strings.TrimSuffix(ref, ".git")

	// Try to match username/repo format
	simplePattern := regexp.MustCompile(`^([a-zA-Z0-9_-]+)/([a-zA-Z0-9_-]+)$`)
	if matches := simplePattern.FindStringSubmatch(ref); matches != nil {
		return &PackageRef{
			User:    matches[1],
			Repo:    matches[2],
			RepoURL: fmt.Sprintf("https://github.com/%s/%s.git", matches[1], matches[2]),
		}, nil
	}

	// Try to match full GitHub URL
	urlPattern := regexp.MustCompile(`(?:https?://)?(?:www\.)?github\.com/([a-zA-Z0-9_-]+)/([a-zA-Z0-9_-]+)`)
	if matches := urlPattern.FindStringSubmatch(ref); matches != nil {
		return &PackageRef{
			User:    matches[1],
			Repo:    matches[2],
			RepoURL: fmt.Sprintf("https://github.com/%s/%s.git", matches[1], matches[2]),
		}, nil
	}

	return nil, fmt.Errorf("invalid package reference format: %s (expected: username/repo or github.com URL)", ref)
}

// cloneRepository clones a git repository to the specified directory
func cloneRepository(repoURL, destDir string) error {
	cmd := exec.Command("git", "clone", "--depth", "1", repoURL, destDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// readAgentMetadata reads and parses the agent.json file from the repository
func readAgentMetadata(repoDir string) (*AgentMetadata, error) {
	metadataPath := filepath.Join(repoDir, "agent.json")

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("agent.json not found - this repository may not be a published agent")
		}
		return nil, fmt.Errorf("failed to read agent.json: %w", err)
	}

	var metadata AgentMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse agent.json: %w", err)
	}

	// Basic validation
	if metadata.Name == "" {
		return nil, errors.New("agent.json is missing required field: name")
	}
	if metadata.Command == "" {
		return nil, errors.New("agent.json is missing required field: command")
	}

	return &metadata, nil
}
