package onboarding

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"opperator/config"
)

// IsFirstRun checks if this is the first time the app is being run
func IsFirstRun() bool {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return true // If we can't get config dir, assume first run
	}

	prefsFile := filepath.Join(configDir, "preferences.yaml")

	if _, err := os.Stat(prefsFile); os.IsNotExist(err) {
		return true
	}

	// Try to read the preferences to check OnboardingComplete flag
	data, err := os.ReadFile(prefsFile)
	if err != nil {
		return true
	}

	var prefs OnboardingConfig
	if err := yaml.Unmarshal(data, &prefs); err != nil {
		return true
	}

	return !prefs.OnboardingComplete
}

// LoadPreferences loads the saved onboarding preferences
func LoadPreferences() (*OnboardingConfig, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return nil, err
	}

	prefsFile := filepath.Join(configDir, "preferences.yaml")
	data, err := os.ReadFile(prefsFile)
	if err != nil {
		return nil, err
	}

	var prefs OnboardingConfig
	if err := yaml.Unmarshal(data, &prefs); err != nil {
		return nil, err
	}

	return &prefs, nil
}

