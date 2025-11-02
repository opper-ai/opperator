package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DaemonConfig represents a single daemon connection configuration
type DaemonConfig struct {
	Name      string `yaml:"name"`
	Address   string `yaml:"address"`
	AuthToken string `yaml:"auth_token,omitempty"`
	Enabled   bool   `yaml:"enabled"`

	// Provider-specific metadata
	Provider       string `yaml:"provider,omitempty"`        // "local", "hetzner", etc.
	HetznerServerID int64  `yaml:"hetzner_server_id,omitempty"` // Hetzner Cloud server ID
	SSHKeyName     string `yaml:"ssh_key_name,omitempty"`   // SSH key name for server access
}

// DaemonRegistry holds all configured daemon connections
type DaemonRegistry struct {
	Daemons []DaemonConfig `yaml:"daemons"`
}

// GetDaemonRegistryPath returns the path to the daemons.yaml file
func GetDaemonRegistryPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "daemons.yaml"), nil
}

// LoadDaemonRegistry loads the daemon registry from disk
// Always includes a default "local" daemon entry
func LoadDaemonRegistry() (*DaemonRegistry, error) {
	registryPath, err := GetDaemonRegistryPath()
	if err != nil {
		return nil, err
	}

	var registry DaemonRegistry

	// If file doesn't exist, start with empty registry
	if _, err := os.Stat(registryPath); os.IsNotExist(err) {
		registry = DaemonRegistry{Daemons: []DaemonConfig{}}
	} else {
		data, err := os.ReadFile(registryPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read daemon registry: %w", err)
		}

		if err := yaml.Unmarshal(data, &registry); err != nil {
			return nil, fmt.Errorf("failed to parse daemon registry: %w", err)
		}

		// Expand environment variables in auth tokens
		for i := range registry.Daemons {
			registry.Daemons[i].AuthToken = expandEnvVars(registry.Daemons[i].AuthToken)
		}
	}

	// Always ensure a default "local" daemon exists
	hasLocal := false
	for _, d := range registry.Daemons {
		if d.Name == "local" {
			hasLocal = true
			break
		}
	}

	if !hasLocal {
		socketPath, _ := GetSocketPath()
		localDaemon := DaemonConfig{
			Name:      "local",
			Address:   fmt.Sprintf("unix://%s", socketPath),
			AuthToken: "",
			Enabled:   true,
		}
		registry.Daemons = append([]DaemonConfig{localDaemon}, registry.Daemons...)
	}

	return &registry, nil
}

// SaveDaemonRegistry saves the daemon registry to disk
func SaveDaemonRegistry(registry *DaemonRegistry) error {
	registryPath, err := GetDaemonRegistryPath()
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(registry)
	if err != nil {
		return fmt.Errorf("failed to marshal daemon registry: %w", err)
	}

	if err := os.WriteFile(registryPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write daemon registry: %w", err)
	}

	return nil
}

// AddDaemon adds a new daemon to the registry
func (r *DaemonRegistry) AddDaemon(daemon DaemonConfig) error {
	// Check if daemon with this name already exists
	for i, d := range r.Daemons {
		if d.Name == daemon.Name {
			// Replace existing daemon
			r.Daemons[i] = daemon
			return nil
		}
	}

	// Add new daemon
	r.Daemons = append(r.Daemons, daemon)
	return nil
}

// RemoveDaemon removes a daemon from the registry by name
func (r *DaemonRegistry) RemoveDaemon(name string) error {
	for i, d := range r.Daemons {
		if d.Name == name {
			r.Daemons = append(r.Daemons[:i], r.Daemons[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("daemon '%s' not found", name)
}

// GetDaemon returns a daemon by name
func (r *DaemonRegistry) GetDaemon(name string) (*DaemonConfig, error) {
	for _, d := range r.Daemons {
		if d.Name == name {
			return &d, nil
		}
	}
	return nil, fmt.Errorf("daemon '%s' not found", name)
}

// expandEnvVars expands environment variables in the format ${VAR_NAME}
func expandEnvVars(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}

	return os.Expand(s, func(key string) string {
		return os.Getenv(key)
	})
}

// ValidateAddress validates a daemon address format
func ValidateAddress(address string) error {
	if address == "" {
		return fmt.Errorf("address cannot be empty")
	}

	// Check for supported schemes
	if !strings.HasPrefix(address, "unix://") && !strings.HasPrefix(address, "tcp://") {
		return fmt.Errorf("address must start with 'unix://' or 'tcp://', got: %s", address)
	}

	return nil
}
