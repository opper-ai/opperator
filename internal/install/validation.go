package install

import (
	"errors"
	"os/exec"

	"opperator/internal/agent"
)

// checkPrerequisites validates that all required tools are available
func checkPrerequisites() error {
	// Check if git is installed
	if !commandExists("git") {
		return errors.New("git is not installed. Please install git to continue")
	}

	// Note: Daemon check is done when starting the agent
	// No need to check daemon here as the agent can be installed without it running

	return nil
}

// checkAgentExists checks if an agent with the given name already exists
func checkAgentExists(agentName string, configPath string) (bool, error) {
	cfg, err := agent.LoadConfig(configPath)
	if err != nil {
		// If config doesn't exist, agent doesn't exist
		return false, nil
	}

	for _, a := range cfg.Agents {
		if a.Name == agentName {
			return true, nil
		}
	}

	return false, nil
}

// commandExists checks if a command is available in PATH
func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
