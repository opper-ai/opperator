package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"tui/internal/plan"
	"tui/internal/pubsub"
)

//go:embed plan.md
var planDescription []byte

const (
	PlanToolName = "manage_plan"
	planDelay    = 1 * time.Millisecond
)

// PlanItem represents a single plan item (re-exported from plan package)
type PlanItem = plan.PlanItem

// PlanEvent contains the updated plan for pubsub
type PlanEvent struct {
	AgentName     string
	Items         []PlanItem
	Specification string
}

var planBroker = pubsub.NewBroker[PlanEvent]()

var currentFocusedAgent string

// SetCurrentFocusedAgent updates the currently focused agent for the plan tool
func SetCurrentFocusedAgent(agentName string) {
	currentFocusedAgent = agentName
}

type PlanParams struct {
	Action        string   `json:"action"`
	Text          string   `json:"text,omitempty"`
	ID            string   `json:"id,omitempty"`
	IDs           []string `json:"ids,omitempty"`
	Items         []string `json:"items,omitempty"`
	Specification string   `json:"specification,omitempty"`
}

type PlanMetadata struct {
	Action         string     `json:"action"`
	Items          []PlanItem `json:"items"`
	Specification  string     `json:"specification,omitempty"`
	Timestamp      string     `json:"timestamp"`
	ItemsCount     int        `json:"items_count"`
	AffectedItem   *PlanItem  `json:"affected_item,omitempty"`   // For single toggle/remove/add actions
	AffectedItems  []PlanItem `json:"affected_items,omitempty"`  // For batch toggle/remove/add actions
	AffectedCount  int        `json:"affected_count,omitempty"`  // For batch operations
	ClearedCount   int        `json:"cleared_count,omitempty"`   // For clear action
}

// planStore and sessionID will be injected via SetPlanContext
var planStore *plan.Store
var sessionID string

// SetPlanContext injects the plan store and session ID for database operations
func SetPlanContext(store *plan.Store, session string) {
	planStore = store
	sessionID = session
}

func PlanSpec() Spec {
	return Spec{
		Name:        PlanToolName,
		Description: strings.TrimSpace(string(planDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"description": "Action to perform: 'get', 'set_specification', 'add', 'toggle', 'remove', 'clear', or 'list'",
					"enum":        []string{"get", "set_specification", "add", "toggle", "remove", "clear", "list"},
				},
				"specification": map[string]any{
					"type":        "string",
					"description": "Specification/overview of the task (required for 'set_specification' action)",
				},
				"text": map[string]any{
					"type":        "string",
					"description": "Text for the plan item (for single 'add' action, use 'items' array for batch add)",
				},
				"items": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Array of item texts (for batch 'add' action)",
				},
				"id": map[string]any{
					"type":        "string",
					"description": "ID of the plan item (for single 'toggle' and 'remove' actions, use 'ids' array for batch)",
				},
				"ids": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Array of item IDs (for batch 'toggle' and 'remove' actions)",
				},
			},
			"required": []string{"action"},
		},
	}
}

func RunPlan(ctx context.Context, arguments string) (string, string) {
	if err := sleepWithCancel(ctx, planDelay); err != nil {
		return "canceled", ""
	}

	var params PlanParams
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return fmt.Sprintf("Error: invalid parameters: %v", err), ""
	}

	// Get the currently focused agent
	focusedAgent := currentFocusedAgent

	// Validate focused agent
	if strings.TrimSpace(focusedAgent) == "" {
		return "Error: No agent is currently focused. Use the focus_agent tool to focus on an agent first.", ""
	}

	// Validate plan store is initialized
	if planStore == nil {
		return "Error: Plan store not initialized", ""
	}

	// Get current plan
	currentPlan, err := planStore.GetPlan(ctx, sessionID, focusedAgent)
	if err != nil {
		return fmt.Sprintf("Error: failed to get plan: %v", err), ""
	}

	// Perform action and build metadata
	meta := PlanMetadata{
		Action:        params.Action,
		Items:         currentPlan.Items,
		Specification: currentPlan.Specification,
		Timestamp:     time.Now().Format(time.RFC3339),
		ItemsCount:    len(currentPlan.Items),
	}

	switch params.Action {
	case "get":
		// Just return the current state in metadata
		// No additional action needed

	case "set_specification":
		if strings.TrimSpace(params.Specification) == "" {
			return "Error: 'specification' parameter is required for 'set_specification' action", ""
		}
		if err := planStore.SetSpecification(ctx, sessionID, focusedAgent, strings.TrimSpace(params.Specification)); err != nil {
			return fmt.Sprintf("Error: failed to set specification: %v", err), ""
		}
		currentPlan.Specification = strings.TrimSpace(params.Specification)
		meta.Specification = currentPlan.Specification

	case "add":
		// Support both single and batch add
		if len(params.Items) > 0 {
			// Batch add
			addedItems := []PlanItem{}
			for _, itemText := range params.Items {
				if strings.TrimSpace(itemText) == "" {
					continue
				}
				newItem, err := planStore.AddItem(ctx, sessionID, focusedAgent, strings.TrimSpace(itemText))
				if err != nil {
					return fmt.Sprintf("Error: failed to add item: %v", err), ""
				}
				addedItems = append(addedItems, newItem)
			}
			// Refresh plan to get all items
			currentPlan, _ = planStore.GetPlan(ctx, sessionID, focusedAgent)
			meta.Items = currentPlan.Items
			meta.ItemsCount = len(currentPlan.Items)
			meta.AffectedItems = addedItems
			meta.AffectedCount = len(addedItems)
		} else {
			// Single add
			if strings.TrimSpace(params.Text) == "" {
				return "Error: 'text' or 'items' parameter is required for 'add' action", ""
			}
			newItem, err := planStore.AddItem(ctx, sessionID, focusedAgent, strings.TrimSpace(params.Text))
			if err != nil {
				return fmt.Sprintf("Error: failed to add item: %v", err), ""
			}
			currentPlan.Items = append(currentPlan.Items, newItem)
			meta.Items = currentPlan.Items
			meta.ItemsCount = len(currentPlan.Items)
			meta.AffectedItem = &newItem
		}

	case "toggle":
		// Support both single and batch toggle
		if len(params.IDs) > 0 {
			// Batch toggle
			toggledItems := []PlanItem{}
			for _, itemID := range params.IDs {
				if strings.TrimSpace(itemID) == "" {
					continue
				}
				item, err := planStore.ToggleItem(ctx, sessionID, focusedAgent, itemID)
				if err != nil {
					return fmt.Sprintf("Error: %v", err), ""
				}
				toggledItems = append(toggledItems, *item)
			}
			// Refresh plan to get updated items
			currentPlan, _ = planStore.GetPlan(ctx, sessionID, focusedAgent)
			meta.Items = currentPlan.Items
			meta.ItemsCount = len(currentPlan.Items)
			meta.AffectedItems = toggledItems
			meta.AffectedCount = len(toggledItems)
		} else {
			// Single toggle
			if strings.TrimSpace(params.ID) == "" {
				return "Error: 'id' or 'ids' parameter is required for 'toggle' action", ""
			}
			item, err := planStore.ToggleItem(ctx, sessionID, focusedAgent, params.ID)
			if err != nil {
				return fmt.Sprintf("Error: %v", err), ""
			}
			// Refresh plan to get updated items
			currentPlan, _ = planStore.GetPlan(ctx, sessionID, focusedAgent)
			meta.Items = currentPlan.Items
			meta.ItemsCount = len(currentPlan.Items)
			meta.AffectedItem = item
		}

	case "remove":
		// Support both single and batch remove
		if len(params.IDs) > 0 {
			// Batch remove
			removedItems := []PlanItem{}
			for _, itemID := range params.IDs {
				if strings.TrimSpace(itemID) == "" {
					continue
				}
				item, err := planStore.RemoveItem(ctx, sessionID, focusedAgent, itemID)
				if err != nil {
					return fmt.Sprintf("Error: %v", err), ""
				}
				removedItems = append(removedItems, *item)
			}
			// Refresh plan to get updated items
			currentPlan, _ = planStore.GetPlan(ctx, sessionID, focusedAgent)
			meta.Items = currentPlan.Items
			meta.ItemsCount = len(currentPlan.Items)
			meta.AffectedItems = removedItems
			meta.AffectedCount = len(removedItems)
		} else {
			// Single remove
			if strings.TrimSpace(params.ID) == "" {
				return "Error: 'id' or 'ids' parameter is required for 'remove' action", ""
			}
			item, err := planStore.RemoveItem(ctx, sessionID, focusedAgent, params.ID)
			if err != nil {
				return fmt.Sprintf("Error: %v", err), ""
			}
			// Refresh plan to get updated items
			currentPlan, _ = planStore.GetPlan(ctx, sessionID, focusedAgent)
			meta.Items = currentPlan.Items
			meta.ItemsCount = len(currentPlan.Items)
			meta.AffectedItem = item
		}

	case "clear":
		count, err := planStore.Clear(ctx, sessionID, focusedAgent)
		if err != nil {
			return fmt.Sprintf("Error: failed to clear items: %v", err), ""
		}
		currentPlan.Items = []PlanItem{}
		meta.Items = currentPlan.Items
		meta.ItemsCount = 0
		meta.ClearedCount = count

	case "list":
		// Just return the current state in metadata
		// No additional action needed

	default:
		return fmt.Sprintf("Error: unknown action '%s'", params.Action), ""
	}

	// Publish event to update UI (excluding specification)
	PublishPlanEvent(focusedAgent, currentPlan.Items, currentPlan.Specification)

	// Return empty content string - renderer will build messages from metadata
	metaBytes, _ := json.Marshal(meta)
	return "", string(metaBytes)
}

// PublishPlanEvent publishes a plan event to update the UI
func PublishPlanEvent(agentName string, items []PlanItem, specification string) {
	planBroker.Publish(pubsub.UpdatedEvent, PlanEvent{
		AgentName:     agentName,
		Items:         items,
		Specification: specification,
	})
}

// SubscribePlanEvents returns a channel that receives plan events
func SubscribePlanEvents(ctx context.Context) <-chan pubsub.Event[PlanEvent] {
	return planBroker.Subscribe(ctx)
}
