package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"opperator/config"
)

var errEmptySecretName = errors.New("secret name cannot be empty")

func registryPath() (string, error) {
	dir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "secrets.json"), nil
}

func loadRegistry() ([]string, error) {
	path, err := registryPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	if len(data) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(data, &names); err != nil {
		return nil, fmt.Errorf("decode secrets registry: %w", err)
	}
	return names, nil
}

func saveRegistry(names []string) error {
	path, err := registryPath()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	sort.Strings(names)
	data, err := json.MarshalIndent(names, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func RegisterSecret(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errEmptySecretName
	}
	names, err := loadRegistry()
	if err != nil {
		return err
	}
	for _, existing := range names {
		if existing == trimmed {
			return nil
		}
	}
	names = append(names, trimmed)
	return saveRegistry(names)
}

func UnregisterSecret(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errEmptySecretName
	}
	names, err := loadRegistry()
	if err != nil {
		return err
	}
	out := names[:0]
	removed := false
	for _, existing := range names {
		if existing == trimmed {
			removed = true
			continue
		}
		out = append(out, existing)
	}
	if !removed {
		return nil
	}
	return saveRegistry(out)
}

func ListSecrets() ([]string, error) {
	names, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	exists, err := HasSecret(OpperAPIKeyName)
	if err != nil {
		return nil, err
	}
	if exists {
		seen := false
		for _, existing := range names {
			if existing == OpperAPIKeyName {
				seen = true
				break
			}
		}
		if !seen {
			names = append(names, OpperAPIKeyName)
		}
	}
	sort.Strings(names)
	return names, nil
}
