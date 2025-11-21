package publish

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"opperator/config"
	"opperator/internal/agent"
)

type PublishOptions struct {
	SkipScan bool
}

// PublishAgent runs the interactive wizard and pushes the selected agent to GitHub.
func PublishAgent(agentName string, opts PublishOptions) error {
	if strings.TrimSpace(agentName) == "" {
		return fmt.Errorf("agent name is required")
	}

	configPath, err := config.GetConfigFile()
	if err != nil {
		return fmt.Errorf("failed to locate agents.yaml: %w", err)
	}

	rawConfig, err := agent.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load agents.yaml: %w", err)
	}

	// First, check if agent is running on a remote daemon
	// This needs to happen before config check because remote agents might not be in local config
	if err := checkAgentLocation(agentName); err != nil {
		return err
	}

	var selectedCfg *agent.AgentConfig
	for _, a := range rawConfig.Agents {
		if a.Name == agentName {
			cfg := a
			selectedCfg = &cfg
			break
		}
	}
	if selectedCfg == nil {
		return fmt.Errorf("agent %q not found in agents.yaml", agentName)
	}

	agentDir, err := resolveAgentDir(agentName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(agentDir); err != nil {
		return fmt.Errorf("agent directory missing: %w", err)
	}

	state := detectGitHub()

	// Only block if git command itself is not available
	if !commandExists("git") {
		err := fmt.Errorf("git is not installed. Please install git to continue")
		printError(err)
		return err
	}

	// Warn if GitHub CLI is not available, but continue
	if !state.CLIInstalled || !state.Connected {
		printWarning(fmt.Errorf("%s", state.Message))
	}

	repoDefault := fmt.Sprintf("opperator-%s", agentName)
	input := wizardInput{
		GitHubUser:   state.DetectedUser,
		RepoName:     repoDefault,
		Visibility:   "public",
		RunScan:      !opts.SkipScan,
		AccountState: state,
	}

	if err := runWizard(agentName, &input); err != nil {
		return err
	}

	// We no longer block if GitHub CLI is not available - we fallback to manual setup
	packageName := fmt.Sprintf("%s/%s", input.GitHubUser, input.RepoName)

	// optional security scan
	var scan securityScanResult
	if input.RunScan {
		scan, err = runSecurityScanWithSpinner(agentDir)
		if err != nil {
			proceed, confirmErr := confirmProceedAfterScanError(err)
			if confirmErr != nil {
				return confirmErr
			}
			if !proceed {
				return errors.New("publish cancelled because scan did not complete")
			}
			// Show that user proceeded without scan
			printInfo("Proceeded without security scan")
		} else {
			printFindings(scan)
			if len(scan.Findings) > 0 {
				proceed, err := confirmProceedAfterFindings()
				if err != nil {
					return err
				}
				if !proceed {
					return errors.New("publish cancelled to address findings (use Builder agent to refactor)")
				}
				// Show that user proceeded with findings
				printInfo("Proceeded with findings")
			}
		}
	}

	var tempDir string
	err = runStepWithSpinner("Creating staging directory...", func() error {
		var mkdirErr error
		tempDir, mkdirErr = os.MkdirTemp("", "op-publish-*")
		return mkdirErr
	})
	if err != nil {
		printError(fmt.Errorf("failed to create staging dir: %w", err))
		return fmt.Errorf("failed to create staging dir: %w", err)
	}

	// Only clean up temp dir if GitHub CLI is available (auto-push succeeded)
	// Otherwise keep it for manual push
	if state.CLIInstalled && state.Connected {
		defer os.RemoveAll(tempDir)
	}

	err = runStepWithSpinner("Copying agent files...", func() error {
		return copyAgent(agentDir, tempDir)
	})
	if err != nil {
		printError(fmt.Errorf("failed to stage agent: %w", err))
		return fmt.Errorf("failed to stage agent: %w", err)
	}

	// Detect secrets once for use in both metadata and README
	secretNames, hasDynamic, secretErr := agent.IdentifyAgentSecrets(selectedCfg, agentDir)
	if secretErr != nil {
		// Non-fatal: continue with empty secrets list
		secretNames = []string{}
		hasDynamic = false
	}

	// Generate agent.json metadata file
	err = runStepWithSpinner("Generating agent metadata...", func() error {
		metadata := generateAgentMetadata(selectedCfg, secretNames, "1.0.0")
		return writeAgentMetadata(metadata, tempDir)
	})
	if err != nil {
		printError(fmt.Errorf("failed to generate metadata: %w", err))
		return fmt.Errorf("failed to generate metadata: %w", err)
	}

	var readme string
	err = runStepWithSpinner("Generating README...", func() error {
		readme = buildReadme(readmeData{
			AgentName:         agentName,
			Description:       selectedCfg.Description,
			PackageName:       packageName,
			RepoName:          input.RepoName,
			Secrets:           secretNames,
			HasDynamicSecrets: hasDynamic,
			AgentsYAML:        buildAgentsYAMLSnippet(selectedCfg),
		})
		if err := os.WriteFile(filepath.Join(tempDir, "README.md"), []byte(readme), 0644); err != nil {
			return err
		}
		return writeGitignore(tempDir)
	})
	if err != nil {
		printError(fmt.Errorf("failed to generate files: %w", err))
		return fmt.Errorf("failed to generate files: %w", err)
	}

	err = runStepWithSpinner("Initializing git repository...", func() error {
		return initRepo(tempDir, input.RepoName, packageName, input.Visibility, selectedCfg.Description, state)
	})
	if err != nil {
		printError(err)
		return err
	}

	// Only attempt to push if GitHub CLI is available
	if state.CLIInstalled && state.Connected {
		err = runStepWithSpinner("Pushing to GitHub...", func() error {
			return pushRepo(tempDir, state, packageName)
		})
		if err != nil {
			printError(err)
			return err
		}
	}

	printSuccessMessage(state, packageName, input.RepoName, tempDir, len(scan.Findings))

	return nil
}
