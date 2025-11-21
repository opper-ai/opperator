package publish

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type githubState struct {
	DetectedUser string
	Connected    bool
	CLIInstalled bool
	Message      string
}

func detectGitHub() githubState {
	state := githubState{
		CLIInstalled: commandExists("gh"),
	}

	if state.CLIInstalled {
		home, _ := os.UserHomeDir()
		hostsFile := filepath.Join(home, ".config", "gh", "hosts.yml")
		data, err := os.ReadFile(hostsFile)
		if err == nil {
			var parsed map[string]struct {
				User string `yaml:"user"`
			}
			if yamlErr := yaml.Unmarshal(data, &parsed); yamlErr == nil {
				if host, ok := parsed["github.com"]; ok {
					state.DetectedUser = host.User
					state.Connected = true
				}
			}
		}
	}

	if state.Connected {
		state.Message = fmt.Sprintf("Using GitHub CLI account %s", state.DetectedUser)
	} else if state.CLIInstalled {
		state.Message = "GitHub CLI detected but not logged in. Run: gh auth login --git-protocol ssh --scopes \"repo\""
	} else {
		state.Message = "GitHub CLI not found. Install from https://cli.github.com/ then run: gh auth login --git-protocol ssh --scopes \"repo\""
	}

	return state
}

func initRepo(path, repoName, packageName, visibility, description string, state githubState) error {
	cmds := [][]string{
		{"git", "init"},
		{"git", "add", "."},
		{"git", "commit", "-m", "Publish agent"},
		{"git", "branch", "-M", "main"},
	}
	for _, args := range cmds {
		if err := runCmd(path, args...); err != nil {
			return fmt.Errorf("git setup failed: %w", err)
		}
	}

	if commandExists("gh") && state.Connected {
		flags := []string{"gh", "repo", "create", packageName, "--source", ".", "--remote", "origin", "--push"}
		if visibility == "private" {
			flags = append(flags, "--private")
		} else {
			flags = append(flags, "--public")
		}

		// Add description if provided
		if strings.TrimSpace(description) != "" {
			flags = append(flags, "--description", description)
		}

		// Add homepage URL
		flags = append(flags, "--homepage", "https://github.com/opper-ai/opperator")

		if err := runCmd(path, flags...); err != nil {
			return fmt.Errorf("failed to create GitHub repo via gh: %w", wrapGitHubError(err, packageName))
		}
		return nil
	}

	// Use HTTPS remote by default (easier for users without SSH keys)
	remote := fmt.Sprintf("https://github.com/%s.git", packageName)
	if err := runCmd(path, "git", "remote", "add", "origin", remote); err != nil {
		return fmt.Errorf("failed to add remote: %w", err)
	}
	return nil
}

func pushRepo(path string, state githubState, packageName string) error {
	if commandExists("gh") && state.Connected {
		// gh repo create already pushed
		return nil
	}
	// Attempt plain git push, otherwise instruct user.
	if err := runCmd(path, "git", "push", "-u", "origin", "HEAD"); err != nil {
		return fmt.Errorf("failed to push repo. Configure GitHub auth then rerun publish.\nSuggested: gh auth login --git-protocol ssh --scopes \"repo\" and ensure remote is github.com/%s", packageName)
	}
	return nil
}

func runCmd(dir string, args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return nil
}

func wrapGitHubError(err error, packageName string) error {
	msg := err.Error()
	if strings.Contains(msg, "Name already exists") || strings.Contains(msg, "already exists on this account") {
		return fmt.Errorf("repository %s already exists on your account. Choose a different repo name.", packageName)
	}
	if strings.Contains(msg, "Flag --confirm has been deprecated") {
		return fmt.Errorf("%s (use gh without --confirm)", msg)
	}
	return err
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
