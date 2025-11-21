package agent

// IdentifyAgentSecrets exposes the internal secret scanner so other packages can
// reuse the same detection logic that powers agent transfers.
func IdentifyAgentSecrets(config *AgentConfig, agentDir string) ([]string, bool, error) {
	return identifyAgentSecrets(config, agentDir)
}

// ShouldExcludePath reports whether a path should be excluded when packaging or
// copying agent files (e.g., virtualenvs, cache dirs, git metadata).
func ShouldExcludePath(relPath string) bool {
	return shouldExcludePath(relPath)
}
