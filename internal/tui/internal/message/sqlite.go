package message

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type SQLiteService struct {
	db *sql.DB
}

func NewSQLiteService(db *sql.DB) *SQLiteService {
	return &SQLiteService{db: db}
}

func (s *SQLiteService) Create(ctx context.Context, sessionID string, params CreateMessageParams) (Message, error) {
	metadata, _ := json.Marshal(params.Parts)
	now := time.Now().Unix()

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO messages(session_id, role, metadata, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
		sessionID, params.Role, string(metadata), now, now)
	if err != nil {
		return Message{}, err
	}

	id, _ := res.LastInsertId()
	return Message{
		ID:        fmt.Sprintf("%d", id),
		SessionID: sessionID,
		Role:      params.Role,
		Parts:     params.Parts,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *SQLiteService) List(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, role, metadata, created_at, updated_at 
		 FROM messages WHERE session_id = ? ORDER BY id`,
		sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var id int64
		var role, metadata string
		var created, updated int64

		rows.Scan(&id, &role, &metadata, &created, &updated)

		parts := s.deserializeParts(metadata)

		msgs = append(msgs, Message{
			ID:        fmt.Sprintf("%d", id),
			SessionID: sessionID,
			Role:      Role(role),
			Parts:     parts,
			CreatedAt: created,
			UpdatedAt: updated,
		})
	}

	return msgs, rows.Err()
}

func (s *SQLiteService) DeleteBySession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, sessionID)
	return err
}

func (s *SQLiteService) deserializeParts(metadata string) []ContentPart {
	if metadata == "" {
		return []ContentPart{}
	}

	var rawParts []json.RawMessage
	if err := json.Unmarshal([]byte(metadata), &rawParts); err != nil {
		return []ContentPart{}
	}

	var parts []ContentPart
	for _, raw := range rawParts {
		if part := s.deserializeSinglePart(raw); part != nil {
			parts = append(parts, part)
		}
	}
	return parts
}

func (s *SQLiteService) deserializeSinglePart(raw json.RawMessage) ContentPart {
	var textContent TextContent
	if err := json.Unmarshal(raw, &textContent); err == nil && textContent.Text != "" {
		return textContent
	}

	var toolCall ToolCall
	if err := json.Unmarshal(raw, &toolCall); err == nil && toolCall.ID != "" {
		return toolCall
	}

	var toolResult ToolResult
	if err := json.Unmarshal(raw, &toolResult); err == nil && toolResult.ToolCallID != "" {
		return toolResult
	}

	var turnSummary TurnSummary
	if err := json.Unmarshal(raw, &turnSummary); err == nil {
		if strings.TrimSpace(turnSummary.AgentID) != "" || turnSummary.DurationMilli > 0 || strings.TrimSpace(turnSummary.AgentName) != "" || strings.TrimSpace(turnSummary.AgentColor) != "" {
			return turnSummary
		}
	}

	return nil
}
