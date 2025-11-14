package preferences

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"opperator/pkg/db"
	"opperator/pkg/migration"
)

// Store manages UI preferences persisted to sqlite.
type Store struct {
	db *sql.DB
}

func Open() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(home, ".config", "opperator")
	dbPath := filepath.Join(dir, "opperator.db")

	// Initialize centralized database connection pools
	if err := db.Initialize(dbPath); err != nil {
		return nil, err
	}

	writeDB, err := db.GetWriteDB()
	if err != nil {
		return nil, err
	}

	s := &Store{db: writeDB}

	// Run migrations automatically
	migrationRunner := migration.NewRunner(writeDB)
	if err := migrationRunner.Run(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) Get(ctx context.Context, key string) (string, error) {
	readDB, err := db.GetReadDB()
	if err != nil {
		return "", err
	}

	var value string
	err = readDB.QueryRowContext(ctx,
		`SELECT value FROM ui_preferences WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *Store) Set(ctx context.Context, key, value string) error {
	ts := time.Now().Unix()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ui_preferences(key, value, updated_at) VALUES(?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?`,
		key, value, ts, value, ts)
	return err
}

// GetBool retrieves a boolean preference value
func (s *Store) GetBool(ctx context.Context, key string) (bool, error) {
	value, err := s.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if value == "" {
		return false, nil
	}
	return strconv.ParseBool(value)
}

func (s *Store) SetBool(ctx context.Context, key string, value bool) error {
	return s.Set(ctx, key, strconv.FormatBool(value))
}
