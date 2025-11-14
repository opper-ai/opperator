package conversation

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"opperator/pkg/db"
	"opperator/pkg/migration"
)

type Conversation struct {
	ID               string
	Title            string
	CreatedAt        int64
	ActiveAgent      string
	FocusedAgentName string
}

// Store manages conversation metadata persisted to sqlite.
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

func (s *Store) Create(ctx context.Context, title string) (Conversation, error) {
	if title == "" {
		title = time.Now().Format("Jan 2, 3:04 PM")
	}

	id := fmt.Sprintf("%d", time.Now().UnixNano())
	ts := time.Now().Unix()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO conversations(id, title, created_at) VALUES(?, ?, ?)`,
		id, title, ts)

	return Conversation{ID: id, Title: title, CreatedAt: ts}, err
}

func (s *Store) List(ctx context.Context) ([]Conversation, error) {
	// Use read database for queries
	readDB, err := db.GetReadDB()
	if err != nil {
		return nil, err
	}

	rows, err := readDB.QueryContext(ctx,
		`SELECT id, title, created_at, active_agent, focused_agent_name FROM conversations ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		var agent sql.NullString
		var focusedAgent sql.NullString
		rows.Scan(&c.ID, &c.Title, &c.CreatedAt, &agent, &focusedAgent)
		if agent.Valid {
			c.ActiveAgent = agent.String
		}
		if focusedAgent.Valid {
			c.FocusedAgentName = focusedAgent.String
		}
		convs = append(convs, c)
	}

	return convs, rows.Err()
}

func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id)
	return err
}

func (s *Store) UpdateTitle(ctx context.Context, id, title string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE conversations SET title = ? WHERE id = ?`,
		title, id)
	return err
}

func (s *Store) UpdateActiveAgent(ctx context.Context, id, agent string) error {
	var value interface{}
	if strings.TrimSpace(agent) == "" {
		value = nil
	} else {
		value = agent
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE conversations SET active_agent = ? WHERE id = ?`,
		value, id)
	return err
}

func (s *Store) UpdateFocusedAgent(ctx context.Context, id, focusedAgent string) error {
	var value interface{}
	if strings.TrimSpace(focusedAgent) == "" {
		value = nil
	} else {
		value = focusedAgent
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE conversations SET focused_agent_name = ? WHERE id = ?`,
		value, id)
	return err
}

func (s *Store) Get(ctx context.Context, id string) (Conversation, error) {
	// Use read database for queries
	readDB, err := db.GetReadDB()
	if err != nil {
		return Conversation{}, err
	}

	var c Conversation
	var agent sql.NullString
	var focusedAgent sql.NullString
	row := readDB.QueryRowContext(ctx,
		`SELECT id, title, created_at, active_agent, focused_agent_name FROM conversations WHERE id = ?`, id)
	if err := row.Scan(&c.ID, &c.Title, &c.CreatedAt, &agent, &focusedAgent); err != nil {
		return Conversation{}, err
	}
	if agent.Valid {
		c.ActiveAgent = agent.String
	}
	if focusedAgent.Valid {
		c.FocusedAgentName = focusedAgent.String
	}
	return c, nil
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) DB() *sql.DB { return s.db }
