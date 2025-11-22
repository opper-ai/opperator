package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
	"opperator/config"
	"opperator/internal/agent"
	"opperator/internal/credentials"
)

// InstallOptions contains options for agent installation
type InstallOptions struct {
	CustomName string // Custom local name for the agent
	Force      bool   // Overwrite if agent already exists
	NoStart    bool   // Don't auto-start after install
}

// InstallResult contains information about the installed agent
type InstallResult struct {
	AgentName         string
	ShouldAutoStart   bool
	ConfiguredSecrets int
}

// InstallAgent installs an agent from a GitHub repository
func InstallAgent(packageRef string, opts InstallOptions) (*InstallResult, error) {
	// Step 1: Check prerequisites
	if err := checkPrerequisites(); err != nil {
		PrintError(err)
		return nil, err
	}

	// Step 2: Parse package reference
	pkgRef, err := parsePackageRef(packageRef)
	if err != nil {
		PrintError(err)
		return nil, err
	}

	// Step 3: Clone repository to temporary directory
	var tempDir string
	err = RunStepWithSpinner("Cloning repository...", func() error {
		var mkdirErr error
		tempDir, mkdirErr = os.MkdirTemp("", "op-install-*")
		if mkdirErr != nil {
			return mkdirErr
		}
		return cloneRepository(pkgRef.RepoURL, tempDir)
	})
	if err != nil {
		PrintError(fmt.Errorf("failed to clone repository: %w", err))
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	// Step 4: Read agent metadata
	var metadata *AgentMetadata
	err = RunStepWithSpinner("Reading agent metadata...", func() error {
		var readErr error
		metadata, readErr = readAgentMetadata(tempDir)
		return readErr
	})
	if err != nil {
		PrintError(fmt.Errorf("failed to read agent metadata: %w", err))
		return nil, err
	}

	// Display agent information
	printAgentInfo(metadata)

	// Step 4.5: Trust warning
	trustConfirmed, err := confirmTrustWarning()
	if err != nil {
		return nil, err
	}
	if !trustConfirmed {
		return nil, fmt.Errorf("installation cancelled")
	}

	// Step 4.6: Security scan (recommended)
	runScan, err := confirmSecurityScan()
	if err != nil {
		return nil, err
	}

	if runScan {
		scan, scanErr := RunSecurityScanWithSpinner(tempDir)
		if scanErr != nil {
			proceed, confirmErr := confirmProceedAfterScanError(scanErr)
			if confirmErr != nil {
				return nil, confirmErr
			}
			if !proceed {
				return nil, fmt.Errorf("installation cancelled because scan did not complete")
			}
			PrintInfo("Proceeded without security scan")
		} else {
			PrintSecurityFindings(scan)
			if len(scan.Findings) > 0 {
				proceed, err := confirmProceedAfterFindings()
				if err != nil {
					return nil, err
				}
				if !proceed {
					return nil, fmt.Errorf("installation cancelled due to security findings")
				}
				PrintInfo("Proceeded with security findings")
			}
		}
	}

	// Step 5: Determine agent name
	defaultName := metadata.Name
	if opts.CustomName != "" {
		defaultName = opts.CustomName
	}

	// Step 6: Check if agent already exists
	configPath, err := config.GetConfigFile()
	if err != nil {
		PrintError(fmt.Errorf("failed to get config path: %w", err))
		return nil, err
	}
	exists, err := checkAgentExists(defaultName, configPath)
	if err != nil {
		PrintError(err)
		return nil, err
	}

	if exists && !opts.Force {
		err := fmt.Errorf("agent '%s' already exists (use --force to overwrite)", defaultName)
		PrintError(err)
		return nil, err
	}

	// Step 7: Run wizard
	wizardResult, err := runInstallWizard(metadata, defaultName)
	if err != nil {
		PrintError(fmt.Errorf("wizard cancelled: %w", err))
		return nil, err
	}

	// Step 8: Configure secrets if required
	var secretMappings map[string]string
	var secretsToCreate map[string]string
	var secretRenames map[string]string // Maps old secret name -> new secret name for renaming in files
	if len(metadata.RequiredSecrets) > 0 {
		printSecretsInfo(metadata.RequiredSecrets)
		secretMappings, secretsToCreate, secretRenames, err = runSecretConfigWizard(metadata.RequiredSecrets, wizardResult.AgentName)
		if err != nil {
			PrintError(fmt.Errorf("secret configuration failed: %w", err))
			return nil, err
		}

		// Create new secrets
		if len(secretsToCreate) > 0 {
			err = RunStepWithSpinner("Creating secrets...", func() error {
				for secretName, secretValue := range secretsToCreate {
					if err := credentials.SetSecret(secretName, secretValue); err != nil {
						return fmt.Errorf("failed to set secret %s: %w", secretName, err)
					}
				}
				return nil
			})
			if err != nil {
				PrintError(fmt.Errorf("failed to create secrets: %w", err))
				return nil, err
			}
		}
	}

	// Step 9: Prepare agent directory
	configDir, err := config.GetConfigDir()
	if err != nil {
		PrintError(fmt.Errorf("failed to get config directory: %w", err))
		return nil, err
	}

	agentDir := filepath.Join(configDir, "agents", wizardResult.AgentName)

	// If force mode and agent exists, remove existing directory
	if exists && opts.Force {
		if err := os.RemoveAll(agentDir); err != nil {
			PrintError(fmt.Errorf("failed to remove existing agent: %w", err))
			return nil, err
		}
	}

	// Step 10: Copy agent files
	err = RunStepWithSpinner("Installing agent files...", func() error {
		if err := os.MkdirAll(filepath.Dir(agentDir), 0755); err != nil {
			return fmt.Errorf("failed to create agents directory: %w", err)
		}
		return copyDirectory(tempDir, agentDir)
	})
	if err != nil {
		PrintError(fmt.Errorf("failed to install agent files: %w", err))
		return nil, err
	}

	// Step 10.5: Rename secret references in agent files
	if len(secretRenames) > 0 {
		err = RunStepWithSpinner("Updating secret references...", func() error {
			return renameSecretsInFiles(agentDir, secretRenames)
		})
		if err != nil {
			PrintError(fmt.Errorf("failed to update secret references: %w", err))
			return nil, err
		}
	}

	// Step 11: Setup dependencies (recreate venv for Python agents)
	err = RunStepWithSpinner("Setting up dependencies...", func() error {
		return recreateVirtualEnvironment(agentDir)
	})
	if err != nil {
		PrintError(fmt.Errorf("failed to setup dependencies: %w", err))
		return nil, err
	}

	// Step 12: Create agent config from metadata
	agentConfig := metadataToAgentConfig(metadata, wizardResult.AgentName, agentDir, secretMappings)

	// Step 13: Register agent in config
	err = RunStepWithSpinner("Registering agent...", func() error {
		return registerAgent(agentConfig, configPath, exists && opts.Force)
	})
	if err != nil {
		PrintError(fmt.Errorf("failed to register agent: %w", err))
		return nil, err
	}

	// Step 14: Return result with auto-start preference
	// Success message and auto-start are handled by the CLI layer to avoid circular imports
	return &InstallResult{
		AgentName:         wizardResult.AgentName,
		ShouldAutoStart:   wizardResult.AutoStart && !opts.NoStart,
		ConfiguredSecrets: len(secretMappings),
	}, nil
}

// metadataToAgentConfig converts AgentMetadata to agent.AgentConfig
func metadataToAgentConfig(metadata *AgentMetadata, agentName, agentDir string, secretMappings map[string]string) agent.AgentConfig {
	// Merge environment variables from metadata with secret mappings
	env := make(map[string]string)

	// Copy existing env vars from metadata
	for k, v := range metadata.Env {
		env[k] = v
	}

	// Add secret mappings (env var name -> secret://secret_name)
	for envVarName, secretName := range secretMappings {
		env[envVarName] = fmt.Sprintf("secret://%s", secretName)
	}

	return agent.AgentConfig{
		Name:            agentName,
		Description:     metadata.Description,
		Command:         metadata.Command,
		Args:            metadata.Args,
		ProcessRoot:     agentDir,
		Env:             env,
		AutoRestart:     metadata.AutoRestart,
		MaxRestarts:     metadata.MaxRestarts,
		StartWithDaemon: metadata.StartWithDaemon,
		SystemPrompt:    metadata.SystemPrompt,
	}
}

// registerAgent adds the agent to the config file
func registerAgent(agentConfig agent.AgentConfig, configPath string, overwrite bool) error {
	cfg, err := agent.LoadConfig(configPath)
	if err != nil {
		// If config doesn't exist, create empty one
		cfg = &agent.Config{Agents: []agent.AgentConfig{}}
	}

	// If overwriting, remove existing agent
	if overwrite {
		for i, a := range cfg.Agents {
			if a.Name == agentConfig.Name {
				cfg.Agents = append(cfg.Agents[:i], cfg.Agents[i+1:]...)
				break
			}
		}
	}

	// Add new agent
	cfg.Agents = append(cfg.Agents, agentConfig)

	// Save config (preserve YAML structure)
	data, err := os.ReadFile(configPath)
	if err != nil {
		// If file doesn't exist, create new one
		data, _ = yaml.Marshal(map[string]interface{}{
			"agents": cfg.Agents,
		})
		return os.WriteFile(configPath, data, 0644)
	}

	var rawConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	rawConfig["agents"] = cfg.Agents

	newData, err := yaml.Marshal(rawConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, newData, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Set secrets if any were configured
	for secretName, secretValue := range agentConfig.Env {
		if secretValue != "" {
			// TODO: Use proper secret storage
			// For now, we just set it in the environment
			_ = secretName // Placeholder
		}
	}

	return nil
}

// copyDirectory recursively copies a directory
func copyDirectory(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Skip .git directory and agent.json (metadata file)
		if relPath == ".git" || (info.IsDir() && filepath.Base(path) == ".git") {
			return filepath.SkipDir
		}
		if relPath == "agent.json" {
			return nil
		}

		// Destination path
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Copy file
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(dstPath, data, info.Mode())
	})
}

// renameSecretsInFiles renames secret references in agent source files
func renameSecretsInFiles(agentDir string, secretRenames map[string]string) error {
	if len(secretRenames) == 0 {
		return nil
	}

	// Walk through all files in the agent directory
	return filepath.Walk(agentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Skip binary files, .venv, .git, etc.
		relPath, err := filepath.Rel(agentDir, path)
		if err != nil {
			return err
		}

		// Skip non-source files
		if !shouldProcessFile(relPath) {
			return nil
		}

		// Read file contents
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		originalContent := string(content)
		modifiedContent := originalContent

		// Replace all secret name occurrences using regex for exact matching
		for oldName, newName := range secretRenames {
			// Escape special regex characters in the secret name
			escapedOldName := regexp.QuoteMeta(oldName)

			// Pattern to match the secret name in quotes, ensuring exact match
			// Matches: "SECRET_NAME" or 'SECRET_NAME' but NOT "SECRET_NAME_OTHER"
			// Uses word boundaries or quote boundaries to ensure exact match
			doubleQuotePattern := regexp.MustCompile(`"` + escapedOldName + `"`)
			singleQuotePattern := regexp.MustCompile(`'` + escapedOldName + `'`)

			// Replace with proper quoting
			modifiedContent = doubleQuotePattern.ReplaceAllString(modifiedContent, `"`+newName+`"`)
			modifiedContent = singleQuotePattern.ReplaceAllString(modifiedContent, `'`+newName+`'`)
		}

		// Only write if content changed
		if modifiedContent != originalContent {
			if err := os.WriteFile(path, []byte(modifiedContent), info.Mode()); err != nil {
				return fmt.Errorf("failed to write %s: %w", path, err)
			}
		}

		return nil
	})
}

// shouldProcessFile determines if a file should be processed for secret renaming
func shouldProcessFile(relPath string) bool {
	// Skip .venv, .git, __pycache__, node_modules, etc.
	skipDirs := []string{".venv", ".git", "__pycache__", "node_modules", ".pytest_cache", ".mypy_cache"}
	for _, dir := range skipDirs {
		if strings.HasPrefix(relPath, dir+string(filepath.Separator)) || relPath == dir {
			return false
		}
	}

	// Only process source code files
	ext := strings.ToLower(filepath.Ext(relPath))
	sourceExts := []string{".py", ".js", ".ts", ".jsx", ".tsx", ".go", ".java", ".rb", ".php", ".sh", ".yaml", ".yml", ".json", ".toml"}

	for _, sourceExt := range sourceExts {
		if ext == sourceExt {
			return true
		}
	}

	return false
}

// recreateVirtualEnvironment detects Python agents and recreates their virtual environments
// Copied from internal/agent/transfer.go
func recreateVirtualEnvironment(agentDir string) error {
	// Check if this is a Python agent by looking for pyproject.toml or requirements.txt
	hasPyproject := false
	hasRequirements := false

	if _, err := os.Stat(filepath.Join(agentDir, "pyproject.toml")); err == nil {
		hasPyproject = true
	}
	if _, err := os.Stat(filepath.Join(agentDir, "requirements.txt")); err == nil {
		hasRequirements = true
	}

	// Not a Python agent, skip
	if !hasPyproject && !hasRequirements {
		return nil
	}

	// Create virtual environment
	venvPath := filepath.Join(agentDir, ".venv")
	cmd := exec.Command("python3", "-m", "venv", venvPath)
	cmd.Dir = agentDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create virtual environment: %w\nOutput: %s", err, string(output))
	}

	// Install dependencies
	pipPath := filepath.Join(venvPath, "bin", "pip")

	// Upgrade pip first
	cmd = exec.Command(pipPath, "install", "--upgrade", "pip")
	cmd.Dir = agentDir
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to upgrade pip: %w\nOutput: %s", err, string(output))
	}

	// Install from pyproject.toml (editable mode) if it exists
	if hasPyproject {
		cmd = exec.Command(pipPath, "install", "-e", ".")
		cmd.Dir = agentDir
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to install from pyproject.toml: %w\nOutput: %s", err, string(output))
		}
	} else if hasRequirements {
		// Install from requirements.txt
		cmd = exec.Command(pipPath, "install", "-r", "requirements.txt")
		cmd.Dir = agentDir
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to install from requirements.txt: %w\nOutput: %s", err, string(output))
		}
	}

	return nil
}
