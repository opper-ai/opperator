package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"opperator/templates"

	"gopkg.in/yaml.v3"
)

//go:embed bootstrap_new_agent.md
var bootstrapNewAgentDescription []byte

const (
	BootstrapNewAgentToolName = "bootstrap_new_agent"
	bootstrapNewAgentDelay    = 1 * time.Millisecond
)

type BootstrapNewAgentParams struct {
	AgentName   string `json:"agent_name"`
	Description string `json:"description,omitempty"`
}

type BootstrapNewAgentMetadata struct {
	AgentName string   `json:"agent_name"`
	Action    string   `json:"action"`
	At        string   `json:"at"`
	Steps     []string `json:"steps,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type AgentConfig struct {
	Name        string   `yaml:"name"`
	Command     string   `yaml:"command"`
	Args        []string `yaml:"args"`
	ProcessRoot string   `yaml:"process_root"`
	Description string   `yaml:"description"`
}

type AgentsYAML struct {
	Agents []AgentConfig `yaml:"agents"`
}

func BootstrapNewAgentSpec() Spec {
	return Spec{
		Name:        BootstrapNewAgentToolName,
		Description: strings.TrimSpace(string(bootstrapNewAgentDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_name": map[string]any{
					"type":        "string",
					"description": "Name of the new agent (lowercase, no spaces)",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "Description of what the agent does (will be used in set_description() call in main.py)",
				},
			},
			"required": []string{"agent_name"},
		},
	}
}

func RunBootstrapNewAgent(ctx context.Context, arguments string) (string, string) {
	if err := sleepWithCancel(ctx, bootstrapNewAgentDelay); err != nil {
		return "canceled", ""
	}

	var params BootstrapNewAgentParams
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return fmt.Sprintf("Error: invalid parameters: %v", err), ""
	}

	agentName := strings.TrimSpace(params.AgentName)
	if agentName == "" {
		return "Error: agent_name is required", ""
	}

	// Validate agent name
	if strings.Contains(agentName, " ") || strings.Contains(agentName, "/") {
		return "Error: agent_name cannot contain spaces or slashes", ""
	}

	description := strings.TrimSpace(params.Description)
	if description == "" {
		description = fmt.Sprintf("Agent %s", agentName)
	}

	// Generate the set_description() content for the template
	// This will be used by the Builder agent to pass an LLM-generated description
	generatedDescription := description

	meta := BootstrapNewAgentMetadata{
		AgentName: agentName,
		Action:    "bootstrap",
		At:        time.Now().Format(time.RFC3339),
		Steps:     []string{},
	}

	// Get opperator config directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		meta.Error = fmt.Sprintf("failed to get home directory: %v", err)
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Error: %s", meta.Error), string(mb)
	}

	configDir := filepath.Join(homeDir, ".config", "opperator")
	agentDir := filepath.Join(configDir, "agents", agentName)

	// Check if agent directory already exists
	if _, err := os.Stat(agentDir); err == nil {
		meta.Error = fmt.Sprintf("agent directory already exists: %s", agentDir)
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Error: %s", meta.Error), string(mb)
	}

	// Create agent directory
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		meta.Error = fmt.Sprintf("failed to create agent directory: %v", err)
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Error: %s", meta.Error), string(mb)
	}
	meta.Steps = append(meta.Steps, fmt.Sprintf("Created directory: %s", agentDir))

	// Create SDK directory (remove if exists to ensure fresh SDK)
	sdkDst := filepath.Join(agentDir, "opperator")
	sdkExisted := false
	if _, err := os.Stat(sdkDst); err == nil {
		sdkExisted = true
		if err := os.RemoveAll(sdkDst); err != nil {
			meta.Error = fmt.Sprintf("failed to remove existing SDK directory: %v", err)
			mb, _ := json.Marshal(meta)
			return fmt.Sprintf("Error: %s", meta.Error), string(mb)
		}
	}
	if err := os.MkdirAll(sdkDst, 0755); err != nil {
		meta.Error = fmt.Sprintf("failed to create SDK directory: %v", err)
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Error: %s", meta.Error), string(mb)
	}

	// Write embedded SDK files
	sdkFiles := []string{
		"__init__.py",
		"agent.py",
		"lifecycle.py",
		"protocol.py",
		"secrets.py",
	}
	for _, fileName := range sdkFiles {
		embedPath := filepath.Join("python-base/opperator", fileName)
		content, err := templates.FS.ReadFile(embedPath)
		if err != nil {
			meta.Error = fmt.Sprintf("failed to read embedded SDK file %s: %v", fileName, err)
			mb, _ := json.Marshal(meta)
			return fmt.Sprintf("Error: %s", meta.Error), string(mb)
		}
		dstPath := filepath.Join(sdkDst, fileName)
		if err := os.WriteFile(dstPath, content, 0644); err != nil {
			meta.Error = fmt.Sprintf("failed to write SDK file %s: %v", fileName, err)
			mb, _ := json.Marshal(meta)
			return fmt.Sprintf("Error: %s", meta.Error), string(mb)
		}
	}
	if sdkExisted {
		meta.Steps = append(meta.Steps, "Replaced existing SDK with fresh embedded files")
	} else {
		meta.Steps = append(meta.Steps, "Copied SDK from embedded files")
	}

	// Write embedded template to main.py
	templateContent, err := templates.FS.ReadFile("python-base/example_minimal_template.py")
	if err != nil {
		meta.Error = fmt.Sprintf("failed to read embedded template: %v", err)
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Error: %s", meta.Error), string(mb)
	}

	// Replace the hardcoded template description with the generated one
	templateStr := string(templateContent)
	oldDescriptionBlock := `self.set_description("A minimal example agent")`
	newDescriptionBlock := fmt.Sprintf(`self.set_description("%s")`, generatedDescription)
	templateStr = strings.Replace(templateStr, oldDescriptionBlock, newDescriptionBlock, 1)

	mainDst := filepath.Join(agentDir, "main.py")
	if err := os.WriteFile(mainDst, []byte(templateStr), 0644); err != nil {
		meta.Error = fmt.Sprintf("failed to write template: %v", err)
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Error: %s", meta.Error), string(mb)
	}
	meta.Steps = append(meta.Steps, "Copied starter template to main.py")

	// Write pyproject.toml
	pyprojectTemplate, err := templates.FS.ReadFile("python-base/pyproject.toml.template")
	if err != nil {
		meta.Error = fmt.Sprintf("failed to read pyproject.toml template: %v", err)
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Error: %s", meta.Error), string(mb)
	}
	pyprojectContent := strings.Replace(string(pyprojectTemplate), "{{AGENT_NAME}}", agentName, -1)
	pyprojectDst := filepath.Join(agentDir, "pyproject.toml")
	if err := os.WriteFile(pyprojectDst, []byte(pyprojectContent), 0644); err != nil {
		meta.Error = fmt.Sprintf("failed to write pyproject.toml: %v", err)
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Error: %s", meta.Error), string(mb)
	}
	meta.Steps = append(meta.Steps, "Created pyproject.toml")

	// Initialize uv project or fallback to venv
	if err := initializeVenv(agentDir); err != nil {
		meta.Error = fmt.Sprintf("failed to initialize virtual environment: %v", err)
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Error: %s", meta.Error), string(mb)
	}
	meta.Steps = append(meta.Steps, "Initialized virtual environment and installed dependencies")

	// Update agents.yaml
	agentsYAMLPath := filepath.Join(configDir, "agents.yaml")
	if err := addAgentToYAML(agentsYAMLPath, agentName, description); err != nil {
		meta.Error = fmt.Sprintf("failed to update agents.yaml: %v", err)
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Error: %s", meta.Error), string(mb)
	}
	meta.Steps = append(meta.Steps, "Registered in agents.yaml")

	// Reload daemon config to pick up new agent
	reloadResp, err := ipcRequestCtx(ctx, struct {
		Type string `json:"type"`
	}{Type: "reload_config"})
	if err != nil {
		meta.Error = fmt.Sprintf("failed to reload daemon config: %v", err)
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Error: %s", meta.Error), string(mb)
	}
	var reloadResult struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(reloadResp, &reloadResult); err != nil || !reloadResult.Success {
		errMsg := reloadResult.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}
		meta.Error = fmt.Sprintf("failed to reload config: %s", errMsg)
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Error: %s", meta.Error), string(mb)
	}
	meta.Steps = append(meta.Steps, "Reloaded daemon configuration")

	// Start the agent via IPC
	respb, err := ipcRequestCtx(ctx, struct {
		Type      string `json:"type"`
		AgentName string `json:"agent_name"`
	}{Type: "start", AgentName: agentName})
	if err != nil {
		meta.Error = fmt.Sprintf("failed to start agent: %v", err)
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Warning: agent created but failed to start: %s", meta.Error), string(mb)
	}
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(respb, &resp); err != nil || !resp.Success {
		errMsg := resp.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}
		meta.Error = errMsg
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Warning: agent created but failed to start: %s", errMsg), string(mb)
	}
	meta.Steps = append(meta.Steps, "Started agent")

	// Focus on the new agent
	PublishFocusAgentEvent(agentName)
	meta.Steps = append(meta.Steps, "Focused on agent")

	mb, _ := json.Marshal(meta)
	return fmt.Sprintf("Successfully bootstrapped agent %q", agentName), string(mb)
}

// initializeVenv creates a virtual environment using uv or fallback to venv,
// then installs the agent as an editable package
func initializeVenv(agentDir string) error {
	venvPath := filepath.Join(agentDir, ".venv")
	venvPython := filepath.Join(venvPath, "bin", "python")

	// Step 1: Create virtual environment
	// Try uv first
	cmd := exec.Command("uv", "venv", ".venv")
	cmd.Dir = agentDir
	_, uvErr := cmd.CombinedOutput()
	uvAvailable := uvErr == nil

	if !uvAvailable {
		// Fallback to python -m venv
		cmd = exec.Command("python3", "-m", "venv", ".venv")
		cmd.Dir = agentDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to create venv: %w (output: %s)", err, string(output))
		}
	}

	// Step 2: Install agent as editable package (makes opperator/ importable)
	if uvAvailable {
		// Use uv pip install with --python flag
		cmd = exec.Command("uv", "pip", "install", "-e", agentDir, "--python", venvPython)
		_, err := cmd.CombinedOutput()
		if err != nil {
			// If uv pip install fails, try pip fallback
			cmd = exec.Command(venvPython, "-m", "pip", "install", "-e", agentDir)
			output, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("failed to install agent package: %w (output: %s)", err, string(output))
			}
		}
	} else {
		// Use pip directly
		cmd = exec.Command(venvPython, "-m", "pip", "install", "-e", agentDir)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to install agent package: %w (output: %s)", err, string(output))
		}
	}

	return nil
}

// addAgentToYAML adds a new agent entry to agents.yaml
func addAgentToYAML(yamlPath, agentName, description string) error {
	var config AgentsYAML

	// Read existing YAML if it exists
	data, err := os.ReadFile(yamlPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &config); err != nil {
			return err
		}
	}

	// Check if agent already exists
	for _, agent := range config.Agents {
		if agent.Name == agentName {
			return fmt.Errorf("agent %q already exists in agents.yaml", agentName)
		}
	}

	// Add new agent
	newAgent := AgentConfig{
		Name:        agentName,
		Command:     ".venv/bin/python",
		Args:        []string{"main.py"},
		ProcessRoot: fmt.Sprintf("agents/%s", agentName),
		Description: description,
	}
	config.Agents = append(config.Agents, newAgent)

	// Write back to file
	data, err = yaml.Marshal(&config)
	if err != nil {
		return err
	}

	return os.WriteFile(yamlPath, data, 0644)
}
