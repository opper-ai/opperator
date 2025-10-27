package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	toolregistry "tui/tools/registry"
)

const AsyncToolName = "daemon_async"

const (
	requestSubmitToolTask = "tool_submit"
	requestGetToolTask    = "tool_get"
	requestListToolTasks  = "tool_list"
	requestDeleteToolTask = "tool_delete"
)

type AsyncTask struct {
	ID          string
	ToolName    string
	Args        string
	WorkingDir  string
	SessionID   string
	CallID      string
	Origin      string
	ClientID    string
	Status      string
	Result      string
	Metadata    string
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time
	Mode        string
	AgentName   string
	CommandName string
	CommandArgs string
	Progress    []AsyncTaskProgress
}

type rawAsyncTask struct {
	ID          string             `json:"id"`
	ToolName    string             `json:"tool_name"`
	Args        string             `json:"args"`
	WorkingDir  string             `json:"working_dir"`
	SessionID   string             `json:"session_id"`
	CallID      string             `json:"call_id"`
	Origin      string             `json:"origin"`
	ClientID    string             `json:"client_id"`
	Status      string             `json:"status"`
	Result      string             `json:"result"`
	Metadata    string             `json:"metadata"`
	Error       string             `json:"error"`
	CreatedAt   string             `json:"created_at"`
	UpdatedAt   string             `json:"updated_at"`
	CompletedAt string             `json:"completed_at"`
	Mode        string             `json:"mode"`
	AgentName   string             `json:"agent_name"`
	CommandName string             `json:"command_name"`
	CommandArgs string             `json:"command_args"`
	Progress    []rawAsyncProgress `json:"progress"`
}

type rawAsyncProgress struct {
	Timestamp string `json:"timestamp"`
	Text      string `json:"text"`
	Metadata  string `json:"metadata"`
	Status    string `json:"status"`
}

type AsyncTaskProgress struct {
	Timestamp time.Time
	Text      string
	Metadata  string
	Status    string
}

func RunAsyncTool(ctx context.Context, arguments string, workingDir string, sessionID string, callID string) (string, string) {
	var params struct {
		Tool          string          `json:"tool"`
		Input         json.RawMessage `json:"input"`
		Mode          string          `json:"mode"`
		Agent         string          `json:"agent"`
		Command       string          `json:"command"`
		CommandArgs   json.RawMessage `json:"command_args"`
		Origin        string          `json:"origin"`
		ClientID      string          `json:"client_id"`
		ProgressLabel string          `json:"progress_label"`
		CommandLabel  string          `json:"command_label"`
	}
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return fmt.Sprintf("error parsing parameters: %v", err), ""
	}
	trimmedTool := strings.TrimSpace(params.Tool)
	if trimmedTool == "" {
		return "error: tool parameter is required", ""
	}
	input := params.Input
	if len(input) == 0 {
		input = []byte("{}")
	}

	payload := map[string]any{
		"type":        requestSubmitToolTask,
		"tool_name":   trimmedTool,
		"tool_args":   string(input),
		"working_dir": strings.TrimSpace(workingDir),
	}
	if trimmed := strings.TrimSpace(params.Mode); trimmed != "" {
		payload["mode"] = trimmed
	}
	if agent := strings.TrimSpace(params.Agent); agent != "" {
		payload["agent_name"] = agent
	}
	if command := strings.TrimSpace(params.Command); command != "" {
		payload["command"] = command
	}
	if len(params.CommandArgs) > 0 {
		payload["command_args"] = string(params.CommandArgs)
	}
	if sessionID != "" {
		payload["session_id"] = sessionID
	}
	if callID != "" {
		payload["call_id"] = callID
	}
	origin := strings.TrimSpace(params.Origin)
	if origin == "" {
		origin = "tui"
	}
	payload["origin"] = origin
	clientID := strings.TrimSpace(params.ClientID)
	if clientID == "" {
		clientID = sessionID
	}
	if clientID != "" {
		payload["client_id"] = clientID
	}

	respBytes, err := IPCRequestCtx(ctx, payload)
	if err != nil {
		return fmt.Sprintf("error submitting async task: %v", err), ""
	}

	var resp struct {
		Success bool          `json:"success"`
		Error   string        `json:"error"`
		Task    *rawAsyncTask `json:"task"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return fmt.Sprintf("error decoding async task response: %v", err), ""
	}
	if !resp.Success {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "unknown async task error"
		}
		return fmt.Sprintf("error scheduling async task: %s", errMsg), ""
	}
	if resp.Task == nil {
		return "error: daemon returned no task payload", ""
	}

	task, err := decodeAsyncTask(*resp.Task)
	if err != nil {
		return fmt.Sprintf("error decoding async task payload: %v", err), ""
	}

	metaPayload := map[string]any{
		"async_task": map[string]any{
			"id":         task.ID,
			"status":     task.Status,
			"tool":       task.ToolName,
			"session_id": task.SessionID,
			"call_id":    task.CallID,
		},
	}

	context := map[string]any{}
	trimmedAgent := strings.TrimSpace(params.Agent)
	trimmedCommand := strings.TrimSpace(params.Command)
	trimmedLabel := strings.TrimSpace(params.CommandLabel)
	if trimmedLabel == "" {
		trimmedLabel = strings.TrimSpace(params.ProgressLabel)
	}
	if trimmedLabel == "" {
		trimmedLabel = trimmedCommand
	}
	if trimmedLabel == "" {
		trimmedLabel = strings.TrimSpace(params.Tool)
	}
	if trimmedLabel != "" {
		context["label"] = toolregistry.PrettifyName(trimmedLabel)
	}
	if trimmedCommand != "" {
		context["title"] = toolregistry.PrettifyName(trimmedCommand)
	}
	if trimmedAgent != "" {
		context["name"] = trimmedAgent
		context["subtitle"] = trimmedAgent
	}
	if len(context) > 0 {
		metaPayload["async_context"] = context
		metaPayload["context"] = context
	}
	metaBytes, _ := json.Marshal(metaPayload)
	content := fmt.Sprintf("async task %s scheduled (%s)", task.ID, task.Status)
	return content, string(metaBytes)
}

func FetchAsyncTask(ctx context.Context, taskID string) (*AsyncTask, error) {
	id := strings.TrimSpace(taskID)
	if id == "" {
		return nil, fmt.Errorf("task id is required")
	}
	payload := map[string]any{
		"type":    requestGetToolTask,
		"task_id": id,
	}
	respBytes, err := IPCRequestCtx(ctx, payload)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Success bool          `json:"success"`
		Error   string        `json:"error"`
		Task    *rawAsyncTask `json:"task"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, errors.New(errMsg)
	}
	if resp.Task == nil {
		return nil, fmt.Errorf("task not found")
	}
	task, err := decodeAsyncTask(*resp.Task)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func ListAsyncTasks(ctx context.Context) ([]AsyncTask, error) {
	respBytes, err := IPCRequestCtx(ctx, map[string]any{"type": requestListToolTasks})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Success bool           `json:"success"`
		Error   string         `json:"error"`
		Tasks   []rawAsyncTask `json:"tasks"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, errors.New(errMsg)
	}
	result := make([]AsyncTask, 0, len(resp.Tasks))
	for _, raw := range resp.Tasks {
		task, err := decodeAsyncTask(raw)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, nil
}

func DeleteAsyncTask(ctx context.Context, taskID string) error {
	id := strings.TrimSpace(taskID)
	if id == "" {
		return nil
	}
	return deleteAsyncTasks(ctx, map[string]any{
		"type":    requestDeleteToolTask,
		"task_id": id,
	})
}

func DeleteAsyncTasksBySession(ctx context.Context, sessionID string) error {
	sess := strings.TrimSpace(sessionID)
	if sess == "" {
		return nil
	}
	return deleteAsyncTasks(ctx, map[string]any{
		"type":       requestDeleteToolTask,
		"session_id": sess,
	})
}

func DeleteAsyncTasksByCall(ctx context.Context, callID string) error {
	call := strings.TrimSpace(callID)
	if call == "" {
		return nil
	}
	return deleteAsyncTasks(ctx, map[string]any{
		"type":    requestDeleteToolTask,
		"call_id": call,
	})
}

func deleteAsyncTasks(ctx context.Context, payload map[string]any) error {
	respBytes, err := IPCRequestCtx(ctx, payload)
	if err != nil {
		return err
	}
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return err
	}
	if !resp.Success {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return errors.New(errMsg)
	}
	return nil
}

func decodeAsyncTask(raw rawAsyncTask) (AsyncTask, error) {
	parse := func(value string) (time.Time, error) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return time.Time{}, nil
		}
		return time.Parse(time.RFC3339Nano, trimmed)
	}
	created, err := parse(raw.CreatedAt)
	if err != nil {
		return AsyncTask{}, fmt.Errorf("parse created_at: %w", err)
	}
	updated, err := parse(raw.UpdatedAt)
	if err != nil {
		return AsyncTask{}, fmt.Errorf("parse updated_at: %w", err)
	}
	var completedPtr *time.Time
	if raw.CompletedAt != "" {
		completed, err := parse(raw.CompletedAt)
		if err != nil {
			return AsyncTask{}, fmt.Errorf("parse completed_at: %w", err)
		}
		completedPtr = &completed
	}
	return AsyncTask{
		ID:          strings.TrimSpace(raw.ID),
		ToolName:    strings.TrimSpace(raw.ToolName),
		Args:        raw.Args,
		WorkingDir:  strings.TrimSpace(raw.WorkingDir),
		SessionID:   strings.TrimSpace(raw.SessionID),
		CallID:      strings.TrimSpace(raw.CallID),
		Origin:      strings.TrimSpace(raw.Origin),
		ClientID:    strings.TrimSpace(raw.ClientID),
		Status:      strings.TrimSpace(raw.Status),
		Result:      raw.Result,
		Metadata:    raw.Metadata,
		Error:       raw.Error,
		CreatedAt:   created,
		UpdatedAt:   updated,
		CompletedAt: completedPtr,
		Mode:        strings.TrimSpace(raw.Mode),
		AgentName:   strings.TrimSpace(raw.AgentName),
		CommandName: strings.TrimSpace(raw.CommandName),
		CommandArgs: strings.TrimSpace(raw.CommandArgs),
		Progress:    decodeAsyncProgress(raw.Progress),
	}, nil
}

func decodeAsyncProgress(entries []rawAsyncProgress) []AsyncTaskProgress {
	if len(entries) == 0 {
		return nil
	}
	out := make([]AsyncTaskProgress, 0, len(entries))
	for _, entry := range entries {
		ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(entry.Timestamp))
		if err != nil {
			ts = time.Time{}
		}
		out = append(out, AsyncTaskProgress{
			Timestamp: ts,
			Text:      strings.TrimSpace(entry.Text),
			Metadata:  strings.TrimSpace(entry.Metadata),
			Status:    strings.TrimSpace(entry.Status),
		})
	}
	return out
}
