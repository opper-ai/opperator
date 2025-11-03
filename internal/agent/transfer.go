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
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentPackage represents a transferable agent with all its files
type AgentPackage struct {
	Config     AgentConfig // Agent configuration
	FilesData  []byte      // tar.gz of agent directory
	WasRunning bool        // Whether agent was running before transfer
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

	// Package agent directory if it exists
	var filesData []byte
	if agentConfig.ProcessRoot != "" {
		// Resolve relative paths against config directory
		agentDir := agentConfig.ProcessRoot
		if !filepath.IsAbs(agentDir) {
			configDir := filepath.Dir(configPath)
			agentDir = filepath.Join(configDir, agentDir)
		}

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

	return &AgentPackage{
		Config:     *agentConfig,
		FilesData:  filesData,
		WasRunning: wasRunning,
	}, nil
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
