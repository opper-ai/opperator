package publish

import (
	"strings"
	"testing"
)

func TestBuildReadme(t *testing.T) {
	data := readmeData{
		AgentName:         "test-agent",
		Description:       "A test automation agent",
		PackageName:       "testuser/test-repo",
		RepoName:          "test-repo",
		Secrets:           []string{"API_KEY", "DB_PASSWORD"},
		HasDynamicSecrets: true,
		AgentsYAML:        "agents:\n  - name: test-agent\n    command: python",
	}

	readme := buildReadme(data)

	// Verify key sections are present
	if !strings.Contains(readme, "# test-agent") {
		t.Error("README should contain agent name as title")
	}

	if !strings.Contains(readme, "A test automation agent") {
		t.Error("README should contain description")
	}

	if !strings.Contains(readme, "testuser/test-repo") {
		t.Error("README should contain package name")
	}

	if !strings.Contains(readme, "op install testuser/test-repo") {
		t.Error("README should contain install command")
	}

	if !strings.Contains(readme, "API_KEY") {
		t.Error("README should list API_KEY secret")
	}

	if !strings.Contains(readme, "DB_PASSWORD") {
		t.Error("README should list DB_PASSWORD secret")
	}

	if !strings.Contains(readme, "dynamic `get_secret()` calls") {
		t.Error("README should mention dynamic secrets")
	}

	if !strings.Contains(readme, "agents:\n  - name: test-agent\n    command: python") {
		t.Error("README should contain agents.yaml snippet")
	}
}

func TestBuildReadmeWithDefaults(t *testing.T) {
	data := readmeData{
		AgentName: "",
		RepoName:  "my-repo",
	}

	readme := buildReadme(data)

	if !strings.Contains(readme, "# Opperator Agent") {
		t.Error("README should use default agent name")
	}

	if !strings.Contains(readme, "Automation agent packaged for Opperator. Repository: my-repo.") {
		t.Error("README should use default description")
	}
}

func TestBuildReadmeNoSecrets(t *testing.T) {
	data := readmeData{
		AgentName:         "no-secrets-agent",
		RepoName:          "test-repo",
		Secrets:           []string{},
		HasDynamicSecrets: false,
	}

	readme := buildReadme(data)

	if !strings.Contains(readme, "No secrets were automatically detected") {
		t.Error("README should mention no secrets detected")
	}

	if strings.Contains(readme, "dynamic `get_secret()` calls") {
		t.Error("README should not mention dynamic secrets when not present")
	}
}
