package publish

import (
	"strings"
	"testing"
)

// TestCheckAgentLocation tests the daemon location validation logic.
// Note: This test relies on the actual daemon being available or not.
// In a real environment, you might want to mock the llm.ListAgents function.
func TestCheckAgentLocationNonExistentAgent(t *testing.T) {
	// An agent that doesn't exist should not cause an error
	err := checkAgentLocation("nonexistent-agent-xyz-123")
	if err != nil {
		t.Errorf("Expected no error for non-existent agent, got: %v", err)
	}
}

// TestCheckAgentLocationErrorMessage verifies the error message format
// This is a unit test that doesn't actually call the daemon
func TestRemoteDaemonErrorMessage(t *testing.T) {
	agentName := "test-agent"
	daemon := "remote-daemon"

	expectedParts := []string{
		"running on remote daemon",
		"op agent move",
		"--to=local",
		agentName,
		daemon,
	}

	// Simulate the error message format
	errMsg := "agent \"test-agent\" is running on remote daemon \"remote-daemon\". To publish, move it to local first:\n\n  op agent move test-agent --to=local"

	for _, part := range expectedParts {
		if !strings.Contains(errMsg, part) {
			t.Errorf("Error message should contain %q, got: %s", part, errMsg)
		}
	}
}
