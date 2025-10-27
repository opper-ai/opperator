package credentials

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	serviceName     = "opperator"
	OpperAPIKeyName = "OPPER_API_KEY"
)

// ErrNotFound indicates that a requested secret was not found in the keyring.
var ErrNotFound = errors.New("secret not found")

// GetSecret retrieves the named secret from the system keyring.
func GetSecret(name string) (string, error) {
	secret, err := keyring.Get(serviceName, name)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("read secret %q: %w", name, err)
	}
	return secret, nil
}

func SetSecret(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("secret %q cannot be empty", name)
	}
	if err := keyring.Set(serviceName, name, trimmed); err != nil {
		return fmt.Errorf("store secret %q: %w", name, err)
	}
	return nil
}

func DeleteSecret(name string) error {
	if err := keyring.Delete(serviceName, name); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("delete secret %q: %w", name, err)
	}
	return nil
}

// HasSecret reports whether the named secret exists in the keyring.
func HasSecret(name string) (bool, error) {
	_, err := GetSecret(name)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return false, err
}

// Convenience helpers for the Opper API key.
func GetAPIKey() (string, error) { return GetSecret(OpperAPIKeyName) }

func SetAPIKey(key string) error { return SetSecret(OpperAPIKeyName, key) }

func DeleteAPIKey() error { return DeleteSecret(OpperAPIKeyName) }

func HasAPIKey() (bool, error) {
	exists, err := HasSecret(OpperAPIKeyName)
	if err != nil || !exists {
		return exists, err
	}
	if regErr := RegisterSecret(OpperAPIKeyName); regErr != nil {
		return exists, regErr
	}
	return true, nil
}
