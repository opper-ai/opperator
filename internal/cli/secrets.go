package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"opperator/internal/credentials"
)

func CreateSecret(name, value string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("secret name cannot be empty")
	}
	secret, err := ensureSecretInput(value, fmt.Sprintf("Enter new value for %s: ", name))
	if err != nil {
		return err
	}

	exists, err := credentials.HasSecret(name)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("a secret named %q is already stored; use 'update' to replace it", name)
	}

	if err := credentials.SetSecret(name, secret); err != nil {
		return err
	}
	if err := credentials.RegisterSecret(name); err != nil {
		return err
	}

	fmt.Printf("Stored secret %q in the system keyring\n", name)
	return nil
}

// UpdateSecret replaces the existing secret in the system keyring.
func UpdateSecret(name, value string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("secret name cannot be empty")
	}
	secret, err := ensureSecretInput(value, fmt.Sprintf("Enter replacement value for %s: ", name))
	if err != nil {
		return err
	}

	exists, err := credentials.HasSecret(name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("no secret named %q is stored; use 'create' to add one", name)
	}

	if err := credentials.SetSecret(name, secret); err != nil {
		return err
	}
	if err := credentials.RegisterSecret(name); err != nil {
		return err
	}

	fmt.Printf("Updated secret %q in the system keyring\n", name)
	return nil
}

func DeleteSecret(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("secret name cannot be empty")
	}
	if err := credentials.DeleteSecret(name); err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return fmt.Errorf("no secret named %q is stored", name)
		}
		return err
	}
	if err := credentials.UnregisterSecret(name); err != nil {
		return err
	}

	fmt.Printf("Removed secret %q from the system keyring\n", name)
	return nil
}

// SecretStatus reports whether the named secret exists in the keyring.
func SecretStatus(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("secret name cannot be empty")
	}
	exists, err := credentials.HasSecret(name)
	if err != nil {
		return err
	}
	if exists {
		fmt.Printf("Secret %q is stored in the system keyring\n", name)
	} else {
		fmt.Printf("Secret %q is not stored\n", name)
	}
	return nil
}

// ListSecrets prints all recorded secret names.
func ListSecrets() error {
	names, err := credentials.ListSecrets()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Println("No secrets have been registered yet")
		return nil
	}
	for _, name := range names {
		label := name
		if name == credentials.OpperAPIKeyName {
			label += " (reserved)"
		}
		fmt.Println(label)
	}
	return nil
}

func ReadSecret(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("secret name cannot be empty")
	}
	secret, err := credentials.GetSecret(name)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return "", fmt.Errorf("no secret named %q is stored", name)
		}
		return "", err
	}
	if err := credentials.RegisterSecret(name); err != nil {
		return "", err
	}
	return secret, nil
}

func ensureSecretInput(raw, prompt string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" {
		return trimmed, nil
	}

	fmt.Fprint(os.Stdout, prompt)
	bytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}

	trimmed = strings.TrimSpace(string(bytes))
	if trimmed == "" {
		return "", fmt.Errorf("secret value cannot be empty")
	}

	return trimmed, nil
}
