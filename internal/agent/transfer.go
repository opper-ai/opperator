package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
	"opperator/internal/credentials"
)

// AgentPackage represents a transferable agent with all its files
type AgentPackage struct {
	Config     AgentConfig       // Agent configuration
	FilesData  []byte            // tar.gz of agent directory
	WasRunning bool              // Whether agent was running before transfer
	Secrets    map[string]string // Secrets used by this agent (name -> value)
}

// identifyAgentSecrets finds all secrets referenced by scanning the agent's source code
// Returns (secretNames, hasDynamicSecrets, error)
func identifyAgentSecrets(config *AgentConfig, agentDir string) ([]string, bool, error) {
	secretNames := make(map[string]bool)
	hasGetSecretCall := false

	// If no agent directory, return empty list (no secrets to sync)
	if agentDir == "" {
		return []string{}, false, nil
	}

	// Pattern to match get_secret("SECRET_NAME") or get_secret('SECRET_NAME')
	// Also matches ctx.get_secret(...) for Python agents
	getSecretPattern := regexp.MustCompile(`(?:ctx\.)?get_secret\s*\(\s*["']([^"']+)["']\s*\)`)

	// Pattern to detect any get_secret call (including dynamic ones)
	anyGetSecretPattern := regexp.MustCompile(`(?:ctx\.)?get_secret\s*\(`)

	// Walk through agent directory to find Python files
	err := filepath.Walk(agentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-Python files
		if info.IsDir() || !strings.HasSuffix(path, ".py") {
			return nil
		}

		// Skip excluded paths
		relPath, err := filepath.Rel(agentDir, path)
		if err != nil {
			return err
		}
		if shouldExcludePath(relPath) {
			return nil
		}

		// Read and scan the file
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		contentStr := string(content)

		// Check if there are any get_secret calls at all
		if anyGetSecretPattern.MatchString(contentStr) {
			hasGetSecretCall = true
		}

		// Find all get_secret calls with literal string arguments
		matches := getSecretPattern.FindAllStringSubmatch(contentStr, -1)
		for _, match := range matches {
			if len(match) > 1 {
				secretNames[match[1]] = true
			}
		}

		return nil
	})

	if err != nil {
		return nil, false, fmt.Errorf("failed to scan agent source code: %w", err)
	}

	// Convert map to slice
	var secrets []string
	for name := range secretNames {
		secrets = append(secrets, name)
	}

	// Check if we have dynamic secrets (get_secret calls but no literal names extracted)
	hasDynamicSecrets := hasGetSecretCall && len(secrets) == 0

	// Return detected secrets and whether dynamic secrets were found
	return secrets, hasDynamicSecrets, nil
}

// runTransferWizard shows the complete wizard flow for agent transfer
type transferWizardInput struct {
	SelectedSecrets []string
}

func runTransferWizard(agentName, sourceDaemon, targetDaemon string, detectedSecrets []string, hasDynamicSecrets bool) ([]string, error) {
	// Get all available secrets for selection
	allSecrets, err := credentials.ListSecrets()
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	if len(allSecrets) == 0 {
		// No secrets available at all
		return []string{}, nil
	}

	// Track secrets that exist locally for safe pre-selection
	available := make(map[string]struct{}, len(allSecrets))
	for _, secret := range allSecrets {
		available[secret] = struct{}{}
	}

	// Pre-select detected secrets that we can find locally
	input := &transferWizardInput{
		SelectedSecrets: make([]string, 0, len(detectedSecrets)),
	}
	for _, name := range detectedSecrets {
		if _, ok := available[name]; ok {
			input.SelectedSecrets = append(input.SelectedSecrets, name)
		}
	}

	// Create options for multi-select with detected ones pre-selected
	options := buildSecretOptions(allSecrets)

	// Create theme
	theme := createTransferWizardTheme()
	theme.FieldSeparator = lipgloss.NewStyle().SetString("\n")

	// Build description for intro (avoid markdown parsing on names)
	introDesc := "This will move the agent and perform the following steps:\n\n • Package agent files and configuration\n • Scan for required secrets\n • Sync secrets to destination daemon\n • Transfer agent to destination\n • Remove agent from source"

	// Build description for secret selection
	var secretDesc string
	if hasDynamicSecrets {
		secretDesc = "❕Detected get_secret() calls with dynamic variable names.\n\nCannot automatically determine which secrets are needed.\n\nPlease select the secrets this agent uses.\n"
	} else if len(detectedSecrets) > 0 {
		secretDesc = fmt.Sprintf("Found %d secret(s) used by this agent (pre-selected).\n\nYou can select additional secrets if needed.", len(detectedSecrets))
	} else {
		secretDesc = "No secrets automatically detected.\n\nSelect any secrets this agent needs (if any)."
	}

	// Create form with intro and secret selection
	form := huh.NewForm(
		buildTransferIntroGroup(agentName, introDesc, sourceDaemon, targetDaemon),
		buildSecretSelectionGroup(agentName, sourceDaemon, targetDaemon, secretDesc, options, input),
	).
		WithTheme(theme).
		WithWidth(80).
		WithShowHelp(false).
		WithShowErrors(false)

	if err := form.Run(); err != nil {
		return nil, err
	}

	return input.SelectedSecrets, nil
}

// createTransferWizardTheme creates the theme for the transfer wizard
func createTransferWizardTheme() *huh.Theme {
	primary := lipgloss.Color("#f7c0af")
	fg := lipgloss.Color("#dddddd")
	fgMuted := lipgloss.Color("#7f7f7f")
	fgSubtle := lipgloss.Color("#888888")
	bg := lipgloss.Color("#101012")
	errorColor := lipgloss.Color("#bf5d47")
	success := lipgloss.Color("#87bf47")

	theme := huh.ThemeBase16()
	base := lipgloss.NewStyle().Foreground(fg)

	// Focused field styles
	theme.Focused.Base = base.MarginLeft(0)
	theme.Focused.Title = base.Foreground(primary).Bold(true)
	theme.Focused.Description = base.Foreground(fg)
	theme.Focused.ErrorIndicator = base.Foreground(errorColor)
	theme.Focused.ErrorMessage = base.Foreground(errorColor)

	theme.Form = base.Padding(0)

	// Select/MultiSelect styles
	theme.Focused.SelectSelector = base.Foreground(primary).Bold(true).SetString("> ")
	theme.Focused.MultiSelectSelector = base.Foreground(primary).Bold(true).SetString("> ")
	theme.Focused.SelectedOption = base.Foreground(primary).Bold(true)
	theme.Focused.SelectedPrefix = base.Foreground(success).Bold(true).SetString("✓ ")
	theme.Focused.UnselectedOption = base
	theme.Blurred.UnselectedOption = base.Foreground(errorColor)
	theme.Focused.UnselectedPrefix = lipgloss.NewStyle().SetString("  ")
	theme.Focused.Option = base

	// Button styles
	theme.Focused.FocusedButton = base.Background(primary).Foreground(bg).Bold(true).Padding(0, 2)
	theme.Focused.BlurredButton = base.Foreground(fgMuted).Padding(0).MarginLeft(1)

	// Note/Card styles
	theme.Focused.NoteTitle = base.Foreground(primary).Bold(true)
	theme.Focused.Card = base.Padding(0)
	theme.Focused.Next = base.Background(primary).Foreground(bg).Bold(true).Padding(0, 2).MarginTop(1)

	// Text input styles
	theme.Focused.TextInput.Cursor = base.Foreground(primary)
	theme.Focused.TextInput.Placeholder = base.Foreground(fgSubtle)
	theme.Focused.TextInput.Prompt = base.Foreground(primary)

	// Blurred field styles
	theme.Blurred.Base = base
	theme.Blurred.Title = base.Foreground(fgMuted)
	theme.Blurred.Description = base.Foreground(fg)
	theme.Blurred.NoteTitle = base.Foreground(fgMuted)
	theme.Blurred.TextInput.Placeholder = base.Foreground(fgSubtle)
	theme.Blurred.TextInput.Prompt = base.Foreground(fgMuted)

	// Form-wide styles
	theme.Form = base
	theme.Group = base.Background(lipgloss.Color("#151517")).Padding(0).MarginBottom(0)

	return theme
}

func buildTransferIntroGroup(agentName string, description string, sourceDaemon string, targetDaemon string) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title(fmt.Sprintf("Move Agent %q / %s → %s", agentName, sourceDaemon, targetDaemon)).
			Description(description + "\n").
			Next(true),
	)
}

func buildSecretSelectionGroup(agentName, sourceDaemon, targetDaemon, description string, options []huh.Option[string], input *transferWizardInput) *huh.Group {
	return huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title(fmt.Sprintf("Secrets for %s (%s → %s)", agentName, sourceDaemon, targetDaemon)).
			Description(description).
			Options(options...).
			Value(&input.SelectedSecrets).
			Filterable(false),
	)
}

func buildSecretOptions(allSecrets []string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(allSecrets))
	for _, secret := range allSecrets {
		options = append(options, huh.NewOption(secret, secret))
	}
	return options
}

// collectSecretValues retrieves the values of the specified secrets from the local keyring
func collectSecretValues(secretNames []string) (map[string]string, error) {
	secrets := make(map[string]string)

	for _, name := range secretNames {
		value, err := credentials.GetSecret(name)
		if err != nil {
			if err == credentials.ErrNotFound {
				fmt.Printf("Warning: Secret '%s' not found in keyring, skipping\n", name)
				continue
			}
			return nil, fmt.Errorf("failed to get secret '%s': %w", name, err)
		}
		secrets[name] = value
	}

	return secrets, nil
}

// PackageAgentWithWizard creates a transferable package with an interactive wizard for secret selection
func PackageAgentWithWizard(agentName, sourceDaemon, targetDaemon string, configPath string, wasRunning bool) (*AgentPackage, error) {
	// Load agent configuration
	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Find the agent in config
	var agentConfig *AgentConfig
	for _, a := range config.Agents {
		if a.Name == agentName {
			agentConfig = &a
			break
		}
	}

	if agentConfig == nil {
		return nil, fmt.Errorf("agent '%s' not found in config", agentName)
	}

	// Resolve agent directory path
	var agentDir string
	if agentConfig.ProcessRoot != "" {
		agentDir = agentConfig.ProcessRoot
		if !filepath.IsAbs(agentDir) {
			configDir := filepath.Dir(configPath)
			agentDir = filepath.Join(configDir, agentDir)
		}
	}

	// Scan for secrets first (without showing wizard yet)
	detectedSecrets, hasDynamicSecrets, err := identifyAgentSecrets(agentConfig, agentDir)
	if err != nil {
		return nil, fmt.Errorf("failed to identify secrets: %w", err)
	}

	// Run the wizard for intro and secret selection
	selectedSecrets, err := runTransferWizard(agentName, sourceDaemon, targetDaemon, detectedSecrets, hasDynamicSecrets)
	if err != nil {
		return nil, err
	}

	// Now package the agent directory
	var filesData []byte
	if agentDir != "" {
		if info, err := os.Stat(agentDir); err == nil && info.IsDir() {
			fmt.Printf("Packaging agent directory (excluding .venv and build artifacts)...\n")
			filesData, err = tarGzipDirectory(agentDir)
			if err != nil {
				return nil, fmt.Errorf("failed to package agent directory: %w", err)
			}
			sizeMB := float64(len(filesData)) / (1024 * 1024)
			fmt.Printf("Package size: %.2f MB\n", sizeMB)
		}
	}

	// Collect secret values
	var secrets map[string]string
	if len(selectedSecrets) > 0 {
		secrets, err = collectSecretValues(selectedSecrets)
		if err != nil {
			return nil, fmt.Errorf("failed to collect secrets: %w", err)
		}
		if len(secrets) > 0 {
			fmt.Printf("✓ Collected %d secret(s): %v\n", len(secrets), getSecretNames(secrets))
		}
	}

	return &AgentPackage{
		Config:     *agentConfig,
		FilesData:  filesData,
		WasRunning: wasRunning,
		Secrets:    secrets,
	}, nil
}

// PackageAgent creates a transferable package from an agent
func PackageAgent(agentName string, configPath string, wasRunning bool) (*AgentPackage, error) {
	// Load agent configuration
	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Find the agent in config
	var agentConfig *AgentConfig
	for _, a := range config.Agents {
		if a.Name == agentName {
			agentConfig = &a
			break
		}
	}

	if agentConfig == nil {
		return nil, fmt.Errorf("agent '%s' not found in config", agentName)
	}

	// Resolve agent directory path
	var agentDir string
	if agentConfig.ProcessRoot != "" {
		agentDir = agentConfig.ProcessRoot
		if !filepath.IsAbs(agentDir) {
			configDir := filepath.Dir(configPath)
			agentDir = filepath.Join(configDir, agentDir)
		}
	}

	// Package agent directory if it exists
	var filesData []byte
	if agentDir != "" {
		// Check if directory exists
		if info, err := os.Stat(agentDir); err == nil && info.IsDir() {
			fmt.Printf("Packaging agent directory (excluding .venv and build artifacts)...\n")
			filesData, err = tarGzipDirectory(agentDir)
			if err != nil {
				return nil, fmt.Errorf("failed to package agent directory: %w", err)
			}
			// Show package size
			sizeMB := float64(len(filesData)) / (1024 * 1024)
			fmt.Printf("Package size: %.2f MB\n", sizeMB)
		}
	}

	// Identify and collect secrets used by this agent by scanning source code
	secretNames, _, err := identifyAgentSecrets(agentConfig, agentDir)
	if err != nil {
		return nil, fmt.Errorf("failed to identify secrets: %w", err)
	}

	var secrets map[string]string
	if len(secretNames) > 0 {
		secrets, err = collectSecretValues(secretNames)
		if err != nil {
			return nil, fmt.Errorf("failed to collect secrets: %w", err)
		}
		if len(secrets) > 0 {
			fmt.Printf("✓ Collected %d secret(s): %v\n", len(secrets), getSecretNames(secrets))
		}
	}

	return &AgentPackage{
		Config:     *agentConfig,
		FilesData:  filesData,
		WasRunning: wasRunning,
		Secrets:    secrets,
	}, nil
}

// getSecretNames returns a list of secret names from a secrets map
func getSecretNames(secrets map[string]string) []string {
	names := make([]string, 0, len(secrets))
	for name := range secrets {
		names = append(names, name)
	}
	return names
}

// UnpackageAgent extracts an agent package to the destination
func UnpackageAgent(pkg *AgentPackage, configPath string) error {
	// Load existing config
	config, err := LoadConfig(configPath)
	if err != nil {
		// If config doesn't exist, create empty one
		config = &Config{Agents: []AgentConfig{}}
	}

	// Check if agent already exists
	for _, a := range config.Agents {
		if a.Name == pkg.Config.Name {
			return fmt.Errorf("agent '%s' already exists (use --force to overwrite)", pkg.Config.Name)
		}
	}

	// Extract agent directory if present
	if len(pkg.FilesData) > 0 {
		// Resolve target directory
		agentDir := pkg.Config.ProcessRoot
		if !filepath.IsAbs(agentDir) {
			configDir := filepath.Dir(configPath)
			agentDir = filepath.Join(configDir, agentDir)
		}

		// Create parent directory if needed
		if err := os.MkdirAll(filepath.Dir(agentDir), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		// Extract files
		sizeMB := float64(len(pkg.FilesData)) / (1024 * 1024)
		fmt.Printf("Extracting agent files (%.2f MB)...\n", sizeMB)
		if err := untarGzipDirectory(pkg.FilesData, agentDir); err != nil {
			return fmt.Errorf("failed to extract agent directory: %w", err)
		}
		fmt.Printf("Agent files extracted successfully\n")

		// Recreate virtual environment if this is a Python agent
		if err := recreateVirtualEnvironment(agentDir); err != nil {
			return fmt.Errorf("failed to recreate virtual environment: %w", err)
		}
	}

	// Add agent to config
	config.Agents = append(config.Agents, pkg.Config)

	// Save config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var rawConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Update agents array
	rawConfig["agents"] = config.Agents

	newData, err := yaml.Marshal(rawConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, newData, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// OverwriteAgent replaces an existing agent with a new package
func OverwriteAgent(pkg *AgentPackage, configPath string) error {
	// Load existing config
	config, err := LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Find and replace the agent
	found := false
	for i, a := range config.Agents {
		if a.Name == pkg.Config.Name {
			config.Agents[i] = pkg.Config
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("agent '%s' not found", pkg.Config.Name)
	}

	// Extract agent directory if present
	if len(pkg.FilesData) > 0 {
		// Resolve target directory
		agentDir := pkg.Config.ProcessRoot
		if !filepath.IsAbs(agentDir) {
			configDir := filepath.Dir(configPath)
			agentDir = filepath.Join(configDir, agentDir)
		}

		// Remove old directory if exists
		if _, err := os.Stat(agentDir); err == nil {
			fmt.Printf("Removing old agent directory...\n")
			if err := os.RemoveAll(agentDir); err != nil {
				return fmt.Errorf("failed to remove old directory: %w", err)
			}
		}

		// Create parent directory if needed
		if err := os.MkdirAll(filepath.Dir(agentDir), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		// Extract files
		sizeMB := float64(len(pkg.FilesData)) / (1024 * 1024)
		fmt.Printf("Extracting agent files (%.2f MB)...\n", sizeMB)
		if err := untarGzipDirectory(pkg.FilesData, agentDir); err != nil {
			return fmt.Errorf("failed to extract agent directory: %w", err)
		}
		fmt.Printf("Agent files extracted successfully\n")

		// Recreate virtual environment if this is a Python agent
		if err := recreateVirtualEnvironment(agentDir); err != nil {
			return fmt.Errorf("failed to recreate virtual environment: %w", err)
		}
	}

	// Save config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var rawConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Update agents array
	rawConfig["agents"] = config.Agents

	newData, err := yaml.Marshal(rawConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, newData, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// shouldExcludePath determines if a path should be excluded from packaging
func shouldExcludePath(relPath string) bool {
	// List of patterns to exclude
	excludePatterns := []string{
		".venv/",
		".venv",
		"__pycache__/",
		"__pycache__",
		".git/",
		".git",
		"node_modules/",
		"node_modules",
		".pytest_cache/",
		".pytest_cache",
		".mypy_cache/",
		".mypy_cache",
		".tox/",
		".tox",
		"*.pyc",
		"*.pyo",
		"*.pyd",
		".DS_Store",
	}

	// Normalize path separators
	normalizedPath := filepath.ToSlash(relPath)

	for _, pattern := range excludePatterns {
		// Check if path starts with directory pattern
		if strings.HasSuffix(pattern, "/") {
			if strings.HasPrefix(normalizedPath+"/", pattern) || normalizedPath == strings.TrimSuffix(pattern, "/") {
				return true
			}
		} else if strings.Contains(pattern, "*") {
			// Handle wildcard patterns
			matched, _ := filepath.Match(pattern, filepath.Base(relPath))
			if matched {
				return true
			}
		} else {
			// Exact match or path component match
			if normalizedPath == pattern || strings.HasPrefix(normalizedPath+"/", pattern+"/") {
				return true
			}
		}
	}

	return false
}

// tarGzipDirectory creates a tar.gz archive of a directory, excluding build artifacts
func tarGzipDirectory(sourceDir string) ([]byte, error) {
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzipWriter)

	// Walk the directory
	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// Skip excluded paths
		if shouldExcludePath(relPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip symlinks for safety
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		// Update header name to be relative to source directory
		header.Name = relPath

		// Write header
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		// Write file content if it's a regular file
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			if _, err := io.Copy(tarWriter, file); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if err := tarWriter.Close(); err != nil {
		return nil, err
	}

	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// recreateVirtualEnvironment detects Python agents and recreates their virtual environments
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

	fmt.Printf("Detected Python agent, recreating virtual environment...\n")

	// Create virtual environment
	venvPath := filepath.Join(agentDir, ".venv")
	cmd := exec.Command("python3", "-m", "venv", venvPath)
	cmd.Dir = agentDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create virtual environment: %w\nOutput: %s", err, string(output))
	}

	fmt.Printf("Virtual environment created successfully\n")

	// Install dependencies
	pipPath := filepath.Join(venvPath, "bin", "pip")

	// Upgrade pip first
	fmt.Printf("Upgrading pip...\n")
	cmd = exec.Command(pipPath, "install", "--upgrade", "pip")
	cmd.Dir = agentDir
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to upgrade pip: %w\nOutput: %s", err, string(output))
	}

	// Install from pyproject.toml (editable mode) if it exists
	if hasPyproject {
		fmt.Printf("Installing dependencies from pyproject.toml...\n")
		cmd = exec.Command(pipPath, "install", "-e", ".")
		cmd.Dir = agentDir
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to install from pyproject.toml: %w\nOutput: %s", err, string(output))
		}
		fmt.Printf("Dependencies installed successfully from pyproject.toml\n")
	} else if hasRequirements {
		// Install from requirements.txt
		fmt.Printf("Installing dependencies from requirements.txt...\n")
		cmd = exec.Command(pipPath, "install", "-r", "requirements.txt")
		cmd.Dir = agentDir
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to install from requirements.txt: %w\nOutput: %s", err, string(output))
		}
		fmt.Printf("Dependencies installed successfully from requirements.txt\n")
	}

	return nil
}

// untarGzipDirectory extracts a tar.gz archive to a destination directory
func untarGzipDirectory(data []byte, destDir string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Construct target path
		targetPath := filepath.Join(destDir, header.Name)

		// Security check: ensure target path is within destDir
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(destDir)) {
			return fmt.Errorf("illegal file path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			// Create parent directory if needed
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}

			// Create file
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			// Copy content
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}
