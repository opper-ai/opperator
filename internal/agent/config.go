package agent

import (
	"os"

	"gopkg.in/yaml.v3"
)

type AgentConfig struct {
	Name            string            `yaml:"name"`
	Description     string            `yaml:"description,omitempty"`
	Color           string            `yaml:"color,omitempty"`
	Command         string            `yaml:"command"`
	Args            []string          `yaml:"args"`
	ProcessRoot     string            `yaml:"process_root"`
	Env             map[string]string `yaml:"env"`
	AutoRestart     bool              `yaml:"auto_restart"`
	MaxRestarts     int               `yaml:"max_restarts"`
	StartWithDaemon *bool             `yaml:"start_with_daemon,omitempty"`
	SystemPrompt    string            `yaml:"system_prompt,omitempty"`
}

type Config struct {
	Agents []AgentConfig `yaml:"agents"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}


	return &config, nil
}

// StartWithDaemonEnabled reports whether the agent should start when the daemon launches.
func (c AgentConfig) StartWithDaemonEnabled() bool {
	if c.StartWithDaemon != nil {
		return *c.StartWithDaemon
	}
	return false // default: do NOT auto-start unless explicitly enabled
}
