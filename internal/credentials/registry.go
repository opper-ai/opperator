package credentials

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"opperator/pkg/db"
	"opperator/pkg/migration"
)

var errEmptySecretName = errors.New("secret name cannot be empty")

var (
	initOnce sync.Once
	initErr  error
)

func initDB() error {
	initOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			initErr = err
			return
		}

		dir := filepath.Join(home, ".config", "opperator")
		dbPath := filepath.Join(dir, "opperator.db")

		if err := db.Initialize(dbPath); err != nil {
			initErr = err
			return
		}

		writeDB, err := db.GetWriteDB()
		if err != nil {
			initErr = err
			return
		}

		// Run migrations
		migrationRunner := migration.NewRunner(writeDB)
		if err := migrationRunner.Run(); err != nil {
			initErr = fmt.Errorf("failed to run migrations: %w", err)
			return
		}
	})

	return initErr
}

func RegisterSecret(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errEmptySecretName
	}

	if err := initDB(); err != nil {
		return err
	}

	writeDB, err := db.GetWriteDB()
	if err != nil {
		return err
	}

	ctx := context.Background()
	now := time.Now().Unix()

	_, err = writeDB.ExecContext(ctx,
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

	if err := initDB(); err != nil {
		return err
	}

	writeDB, err := db.GetWriteDB()
	if err != nil {
		return err
	}

	ctx := context.Background()
	_, err = writeDB.ExecContext(ctx, `DELETE FROM secrets WHERE name = ?`, trimmed)
	return err
}

func ListSecrets() ([]string, error) {
	if err := initDB(); err != nil {
		return nil, err
	}

	readDB, err := db.GetReadDB()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	rows, err := readDB.QueryContext(ctx, `SELECT name FROM secrets ORDER BY name`)
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
