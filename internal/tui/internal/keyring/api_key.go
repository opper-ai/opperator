package keyring

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

func GetAPIKey() (string, error) { return GetSecret(OpperAPIKeyName) }

func SetAPIKey(key string) error { return SetSecret(OpperAPIKeyName, key) }

func DeleteAPIKey() error { return DeleteSecret(OpperAPIKeyName) }

func HasAPIKey() (bool, error) { return HasSecret(OpperAPIKeyName) }
