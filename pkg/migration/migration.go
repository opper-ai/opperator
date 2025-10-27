package migration

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Migration struct {
	Version int
	Name    string
	UpSQL   string
	DownSQL string
}

type Runner struct {
	db *sql.DB
}

func NewRunner(db *sql.DB) *Runner {
	return &Runner{db: db}
}

func (r *Runner) Run() error {
	if err := r.ensureSchemaTable(); err != nil {
		return fmt.Errorf("failed to create schema table: %w", err)
	}

	migrations, err := r.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	currentVersion, dirty, err := r.getCurrentVersion()
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	if dirty {
		return fmt.Errorf("database is in dirty state, manual intervention required")
	}

	for _, migration := range migrations {
		if migration.Version <= currentVersion {
			continue
		}

		if err := r.applyMigration(migration); err != nil {
			return fmt.Errorf("failed to apply migration %d: %w", migration.Version, err)
		}
	}

	return nil
}

func (r *Runner) ensureSchemaTable() error {
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			dirty BOOLEAN NOT NULL DEFAULT FALSE,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	return err
}

func (r *Runner) loadMigrations() ([]Migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	migrationMap := make(map[int]*Migration)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}

		version, migrationName, direction, err := r.parseMigrationFilename(name)
		if err != nil {
			continue
		}

		content, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}

		if migrationMap[version] == nil {
			migrationMap[version] = &Migration{
				Version: version,
				Name:    migrationName,
			}
		}

		if direction == "up" {
			migrationMap[version].UpSQL = string(content)
		} else if direction == "down" {
			migrationMap[version].DownSQL = string(content)
		}
	}

	var migrations []Migration
	for _, migration := range migrationMap {
		if migration.UpSQL != "" {
			migrations = append(migrations, *migration)
		}
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

func (r *Runner) parseMigrationFilename(filename string) (version int, name, direction string, err error) {
	name = strings.TrimSuffix(filename, ".sql")
	parts := strings.Split(name, ".")

	if len(parts) != 2 {
		return 0, "", "", fmt.Errorf("invalid migration filename format")
	}

	direction = parts[1]
	if direction != "up" && direction != "down" {
		return 0, "", "", fmt.Errorf("invalid direction: %s", direction)
	}

	nameParts := strings.Split(parts[0], "_")
	if len(nameParts) < 2 {
		return 0, "", "", fmt.Errorf("invalid migration name format")
	}

	version, err = strconv.Atoi(nameParts[0])
	if err != nil {
		return 0, "", "", fmt.Errorf("invalid version number: %w", err)
	}

	name = strings.Join(nameParts[1:], "_")
	return version, name, direction, nil
}

func (r *Runner) getCurrentVersion() (version int, dirty bool, err error) {
	row := r.db.QueryRow(`
		SELECT version, dirty 
		FROM schema_migrations 
		ORDER BY version DESC 
		LIMIT 1
	`)

	err = row.Scan(&version, &dirty)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	return version, dirty, nil
}

func (r *Runner) applyMigration(migration Migration) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO schema_migrations (version, dirty) 
		VALUES (?, TRUE)
	`, migration.Version)
	if err != nil {
		return err
	}

	_, err = tx.Exec(migration.UpSQL)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		UPDATE schema_migrations 
		SET dirty = FALSE 
		WHERE version = ?
	`, migration.Version)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *Runner) Force(version int) error {
	_, err := r.db.Exec(`
		UPDATE schema_migrations 
		SET dirty = FALSE 
		WHERE version = ?
	`, version)
	return err
}
