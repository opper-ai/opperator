package inputhistory

import (
	"context"
	"database/sql"
	"time"
)

// Service provides methods to persist and fetch input history per session.
type Service interface {
	Add(ctx context.Context, sessionID, text string) error
	List(ctx context.Context, sessionID string) ([]string, error)
}

type sqliteService struct {
	db *sql.DB
}

func NewSQLiteService(db *sql.DB) Service { return &sqliteService{db: db} }

func (s *sqliteService) Add(ctx context.Context, sessionID, text string) error {
	if text == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO input_history(session_id, text, created_at) VALUES(?, ?, ?)`,
		sessionID, text, time.Now().Unix(),
	)
	return err
}

func (s *sqliteService) List(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT text FROM input_history ORDER BY created_at`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
