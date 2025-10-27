package plan

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PlanItem represents a single plan item
type PlanItem struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Completed bool   `json:"completed"`
}

// Plan represents a complete plan with specification and items
type Plan struct {
	ID            string
	SessionID     string
	AgentName     string
	Specification string
	Items         []PlanItem
	CreatedAt     int64
	UpdatedAt     int64
}

// Store manages plan data persisted to sqlite.
type Store struct {
	db *sql.DB
}

// NewStore creates a new plan store using an existing database connection
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// GetPlan retrieves the plan for a specific agent in a session
func (s *Store) GetPlan(ctx context.Context, sessionID, agentName string) (*Plan, error) {
	var p Plan
	var itemsJSON string
	var spec sql.NullString

	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, agent_name, specification, items, created_at, updated_at
		 FROM plans WHERE session_id = ? AND agent_name = ?`,
		sessionID, agentName)

	if err := row.Scan(&p.ID, &p.SessionID, &p.AgentName, &spec, &itemsJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			// Return empty plan if not found
			return &Plan{
				SessionID: sessionID,
				AgentName: agentName,
				Items:     []PlanItem{},
			}, nil
		}
		return nil, err
	}

	if spec.Valid {
		p.Specification = spec.String
	}

	if err := json.Unmarshal([]byte(itemsJSON), &p.Items); err != nil {
		return nil, fmt.Errorf("failed to unmarshal items: %w", err)
	}

	if p.Items == nil {
		p.Items = []PlanItem{}
	}

	return &p, nil
}

// SetSpecification sets or updates the specification for a plan
func (s *Store) SetSpecification(ctx context.Context, sessionID, agentName, specification string) error {
	plan, err := s.GetPlan(ctx, sessionID, agentName)
	if err != nil {
		return err
	}

	now := time.Now().Unix()

	if plan.ID == "" {
		// Create new plan
		id := uuid.New().String()
		itemsJSON, _ := json.Marshal([]PlanItem{})

		_, err = s.db.ExecContext(ctx,
			`INSERT INTO plans(id, session_id, agent_name, specification, items, created_at, updated_at)
			 VALUES(?, ?, ?, ?, ?, ?, ?)`,
			id, sessionID, agentName, specification, string(itemsJSON), now, now)
		return err
	}

	// Update existing plan
	_, err = s.db.ExecContext(ctx,
		`UPDATE plans SET specification = ?, updated_at = ? WHERE id = ?`,
		specification, now, plan.ID)
	return err
}

// UpdateItems replaces all items in the plan
func (s *Store) UpdateItems(ctx context.Context, sessionID, agentName string, items []PlanItem) error {
	plan, err := s.GetPlan(ctx, sessionID, agentName)
	if err != nil {
		return err
	}

	if items == nil {
		items = []PlanItem{}
	}

	itemsJSON, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("failed to marshal items: %w", err)
	}

	now := time.Now().Unix()

	if plan.ID == "" {
		// Create new plan
		id := uuid.New().String()

		_, err = s.db.ExecContext(ctx,
			`INSERT INTO plans(id, session_id, agent_name, specification, items, created_at, updated_at)
			 VALUES(?, ?, ?, ?, ?, ?, ?)`,
			id, sessionID, agentName, "", string(itemsJSON), now, now)
		return err
	}

	// Update existing plan
	_, err = s.db.ExecContext(ctx,
		`UPDATE plans SET items = ?, updated_at = ? WHERE id = ?`,
		string(itemsJSON), now, plan.ID)
	return err
}

// AddItem adds a new item to the plan
func (s *Store) AddItem(ctx context.Context, sessionID, agentName, text string) (PlanItem, error) {
	plan, err := s.GetPlan(ctx, sessionID, agentName)
	if err != nil {
		return PlanItem{}, err
	}

	newItem := PlanItem{
		ID:        uuid.New().String(),
		Text:      text,
		Completed: false,
	}

	plan.Items = append(plan.Items, newItem)

	if err := s.UpdateItems(ctx, sessionID, agentName, plan.Items); err != nil {
		return PlanItem{}, err
	}

	return newItem, nil
}

// ToggleItem toggles the completion status of an item
func (s *Store) ToggleItem(ctx context.Context, sessionID, agentName, itemID string) (*PlanItem, error) {
	plan, err := s.GetPlan(ctx, sessionID, agentName)
	if err != nil {
		return nil, err
	}

	for i := range plan.Items {
		if plan.Items[i].ID == itemID {
			plan.Items[i].Completed = !plan.Items[i].Completed

			if err := s.UpdateItems(ctx, sessionID, agentName, plan.Items); err != nil {
				return nil, err
			}

			return &plan.Items[i], nil
		}
	}

	return nil, fmt.Errorf("item with id '%s' not found", itemID)
}

// RemoveItem removes an item from the plan
func (s *Store) RemoveItem(ctx context.Context, sessionID, agentName, itemID string) (*PlanItem, error) {
	plan, err := s.GetPlan(ctx, sessionID, agentName)
	if err != nil {
		return nil, err
	}

	for i, item := range plan.Items {
		if item.ID == itemID {
			removedItem := item
			plan.Items = append(plan.Items[:i], plan.Items[i+1:]...)

			if err := s.UpdateItems(ctx, sessionID, agentName, plan.Items); err != nil {
				return nil, err
			}

			return &removedItem, nil
		}
	}

	return nil, fmt.Errorf("item with id '%s' not found", itemID)
}

// Clear removes all items from the plan
func (s *Store) Clear(ctx context.Context, sessionID, agentName string) (int, error) {
	plan, err := s.GetPlan(ctx, sessionID, agentName)
	if err != nil {
		return 0, err
	}

	count := len(plan.Items)

	if err := s.UpdateItems(ctx, sessionID, agentName, []PlanItem{}); err != nil {
		return 0, err
	}

	return count, nil
}
