package credentials

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"opperator/pkg/migration"

	_ "modernc.org/sqlite"
)

var errEmptySecretName = errors.New("secret name cannot be empty")

var dbInstance *sql.DB

func getDB() (*sql.DB, error) {
	if dbInstance != nil {
		return dbInstance, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(home, ".config", "opperator")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dir, "opperator.db")
	db, err := sql.Open("sqlite", dbPath+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=10000")
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Run migrations
	migrationRunner := migration.NewRunner(db)
	if err := migrationRunner.Run(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	dbInstance = db
	return dbInstance, nil
}

func RegisterSecret(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errEmptySecretName
	}

	db, err := getDB()
	if err != nil {
		return err
	}

	ctx := context.Background()
	now := time.Now().Unix()

	_, err = db.ExecContext(ctx,
		`INSERT INTO secrets(name, created_at, updated_at) VALUES(?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET updated_at = ?`,
		trimmed, now, now, now)
	return err
}

func UnregisterSecret(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errEmptySecretName
	}

	db, err := getDB()
	if err != nil {
		return err
	}

	ctx := context.Background()
	_, err = db.ExecContext(ctx, `DELETE FROM secrets WHERE name = ?`, trimmed)
	return err
}

func ListSecrets() ([]string, error) {
	db, err := getDB()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	rows, err := db.QueryContext(ctx, `SELECT name FROM secrets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Check if OpperAPIKeyName exists in keyring and add it if not in DB
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
			sort.Strings(names)
		}
	}

	return names, nil
}
