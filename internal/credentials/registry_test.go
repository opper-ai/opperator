package credentials

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) func() {
	// Create a temporary directory for the test database
	tmpDir := t.TempDir()

	// Set HOME to the temp directory so our code uses it
	oldHome := os.Getenv("HOME")
	configDir := filepath.Join(tmpDir, ".config", "opperator")
	os.MkdirAll(configDir, 0755)
	os.Setenv("HOME", tmpDir)

	// Reset the dbInstance so we get a fresh connection
	if dbInstance != nil {
		dbInstance.Close()
		dbInstance = nil
	}

	return func() {
		if dbInstance != nil {
			dbInstance.Close()
			dbInstance = nil
		}
		os.Setenv("HOME", oldHome)
	}
}

func TestRegisterSecret(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	err := RegisterSecret("test-secret")
	if err != nil {
		t.Fatalf("RegisterSecret failed: %v", err)
	}

	// Verify it was added to the database
	db, err := getDB()
	if err != nil {
		t.Fatalf("getDB failed: %v", err)
	}

	var name string
	err = db.QueryRow(`SELECT name FROM secrets WHERE name = ?`, "test-secret").Scan(&name)
	if err != nil {
		t.Fatalf("Failed to query secret: %v", err)
	}

	if name != "test-secret" {
		t.Errorf("Expected name to be 'test-secret', got %q", name)
	}
}

func TestUnregisterSecret(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// First register a secret
	err := RegisterSecret("test-secret")
	if err != nil {
		t.Fatalf("RegisterSecret failed: %v", err)
	}

	// Now unregister it
	err = UnregisterSecret("test-secret")
	if err != nil {
		t.Fatalf("UnregisterSecret failed: %v", err)
	}

	// Verify it was removed
	db, err := getDB()
	if err != nil {
		t.Fatalf("getDB failed: %v", err)
	}

	var name string
	err = db.QueryRow(`SELECT name FROM secrets WHERE name = ?`, "test-secret").Scan(&name)
	if err != sql.ErrNoRows {
		t.Errorf("Expected secret to be deleted, but it still exists")
	}
}

func TestListSecrets(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Register multiple secrets
	secrets := []string{"secret1", "secret2", "secret3"}
	for _, secret := range secrets {
		err := RegisterSecret(secret)
		if err != nil {
			t.Fatalf("RegisterSecret(%q) failed: %v", secret, err)
		}
	}

	// List them
	names, err := ListSecrets()
	if err != nil {
		t.Fatalf("ListSecrets failed: %v", err)
	}

	if len(names) != len(secrets) {
		t.Errorf("Expected %d secrets, got %d", len(secrets), len(names))
	}

	// Verify they're sorted
	for i, expected := range secrets {
		if names[i] != expected {
			t.Errorf("Expected secret %d to be %q, got %q", i, expected, names[i])
		}
	}
}

func TestEmptySecretName(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	err := RegisterSecret("")
	if err != errEmptySecretName {
		t.Errorf("Expected errEmptySecretName, got %v", err)
	}

	err = RegisterSecret("   ")
	if err != errEmptySecretName {
		t.Errorf("Expected errEmptySecretName for whitespace, got %v", err)
	}
}
