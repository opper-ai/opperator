package config

import (
	"os"
	"path/filepath"
)

const AppName = "opperator"

func GetConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	configDir := filepath.Join(homeDir, ".config", AppName)

	// Ensure the directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", err
	}

	return configDir, nil
}

func GetConfigFile() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(configDir, "agents.yaml"), nil
}

func GetSocketPath() (string, error) {
	return filepath.Join(os.TempDir(), "opperator.sock"), nil
}

func GetPIDFile() (string, error) {
	return filepath.Join(os.TempDir(), "opperator.pid"), nil
}

func GetDatabasePath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "opperator.db"), nil
}

func GetLogsDir() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}

	logsDir := filepath.Join(configDir, "logs")

	// Ensure the directory exists
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return "", err
	}

	return logsDir, nil
}

func GetDaemonLogPath() (string, error) {
	logsDir, err := GetLogsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(logsDir, "daemon.log"), nil
}

func EnsureConfigExists() error {
	configFile, err := GetConfigFile()
	if err != nil {
		return err
	}

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		defaultConfig := `agents: []
`

		if err := os.WriteFile(configFile, []byte(defaultConfig), 0644); err != nil {
			return err
		}
	}

	return nil
}
