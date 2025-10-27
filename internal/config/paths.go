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
		defaultConfig := `agents:
  - name: "example-server"
    type: "python"
    description: "Serve the current directory over HTTP on port 8080"
    color: "#3ccad7"
    command: "python3"
    args: ["-m", "http.server", "8080"]
    process_root: "./"
    env:
      PYTHONUNBUFFERED: "1"
    auto_restart: false

  - name: "date-logger"
    type: "shell"
    description: "Print the current date every 3 seconds"
    color: "#f97316"
    command: "sh"
    args: ["-c", "while true; do date; sleep 3; done"]
    process_root: "./"
    auto_restart: false

  - name: "file-watcher"
    type: "shell"
    description: "List files in the current directory every 5 seconds"
    color: "#a855f7"
    command: "sh"
    args: ["-c", "ls -la && sleep 5"]
    process_root: "./"
    auto_restart: true
    max_restarts: 3
`

		if err := os.WriteFile(configFile, []byte(defaultConfig), 0644); err != nil {
			return err
		}
	}

	return nil
}
