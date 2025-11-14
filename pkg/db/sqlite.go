package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	_ "modernc.org/sqlite"
)

var (
	readDB  *sql.DB
	writeDB *sql.DB
	once    sync.Once
	initErr error
)

// sqliteDBString constructs a connection string for SQLite with recommended PRAGMA settings
func sqliteDBString(file string, readonly bool) string {
	connectionParams := make(url.Values)
	connectionParams.Add("_journal_mode", "WAL")
	connectionParams.Add("_busy_timeout", "5000")
	connectionParams.Add("_synchronous", "NORMAL")
	connectionParams.Add("_cache_size", "-20000") // 20MB cache
	connectionParams.Add("_foreign_keys", "true")

	if readonly {
		connectionParams.Add("mode", "ro")
	} else {
		connectionParams.Add("_txlock", "immediate")
		connectionParams.Add("mode", "rwc")
	}

	return "file:" + file + "?" + connectionParams.Encode()
}

// openSQLiteDatabase opens a SQLite database with optimized settings
func openSQLiteDatabase(file string, readonly bool) (*sql.DB, error) {
	dbString := sqliteDBString(file, readonly)
	db, err := sql.Open("sqlite", dbString)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set PRAGMAs that can't be set via connection string
	pragmasToSet := []string{
		"temp_store=memory",
		"busy_timeout=10000", // 10 second timeout for lock acquisition
	}

	for _, pragma := range pragmasToSet {
		_, err = db.Exec("PRAGMA " + pragma + ";")
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set PRAGMA %s: %w", pragma, err)
		}
	}

	// Configure connection pool
	if readonly {
		// Read pool: allow multiple concurrent connections
		maxConns := max(4, runtime.NumCPU())
		db.SetMaxOpenConns(maxConns)
		db.SetMaxIdleConns(maxConns)
	} else {
		// Write pool: single connection to serialize writes
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}

	return db, nil
}

// Initialize sets up the read and write database connection pools
func Initialize(dbPath string) error {
	once.Do(func() {
		// Ensure directory exists
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			initErr = fmt.Errorf("failed to create database directory: %w", err)
			return
		}

		// Open write database
		writeDB, initErr = openSQLiteDatabase(dbPath, false)
		if initErr != nil {
			initErr = fmt.Errorf("failed to open write database: %w", initErr)
			return
		}

		// Open read database
		readDB, initErr = openSQLiteDatabase(dbPath, true)
		if initErr != nil {
			writeDB.Close()
			initErr = fmt.Errorf("failed to open read database: %w", initErr)
			return
		}
	})

	return initErr
}

// GetReadDB returns the read-only database connection pool
func GetReadDB() (*sql.DB, error) {
	if readDB == nil {
		return nil, fmt.Errorf("database not initialized, call Initialize() first")
	}
	return readDB, nil
}

// GetWriteDB returns the read-write database connection pool
func GetWriteDB() (*sql.DB, error) {
	if writeDB == nil {
		return nil, fmt.Errorf("database not initialized, call Initialize() first")
	}
	return writeDB, nil
}

// WithTx executes a function within an immediate transaction
func WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	db, err := GetWriteDB()
	if err != nil {
		return err
	}

	// Start an IMMEDIATE transaction to acquire write lock immediately
	// This prevents SQLITE_BUSY errors from deferred lock upgrades
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Close closes both database connection pools
func Close() error {
	var errs []error

	if readDB != nil {
		if err := readDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close read database: %w", err))
		}
	}

	if writeDB != nil {
		if err := writeDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close write database: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing databases: %v", errs)
	}

	return nil
}

// max returns the maximum of two integers (for Go versions < 1.21)
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
