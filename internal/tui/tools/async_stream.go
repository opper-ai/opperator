package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type AsyncTaskEventType string

const (
	AsyncTaskEventSnapshot  AsyncTaskEventType = "snapshot"
	AsyncTaskEventProgress  AsyncTaskEventType = "progress"
	AsyncTaskEventCompleted AsyncTaskEventType = "completed"
	AsyncTaskEventFailed    AsyncTaskEventType = "failed"
	AsyncTaskEventDeleted   AsyncTaskEventType = "deleted"
)

type AsyncTaskEvent struct {
	Type     AsyncTaskEventType
	Task     *AsyncTask
	Progress *AsyncTaskProgress
	Error    string
}

type wireToolTaskEvent struct {
	Type     string            `json:"type"`
	Task     *wireToolTask     `json:"task,omitempty"`
	Progress *wireToolProgress `json:"progress,omitempty"`
	Error    string            `json:"error,omitempty"`
}

type wireToolTask struct {
	ID          string             `json:"id"`
	ToolName    string             `json:"tool_name"`
	Args        string             `json:"args"`
	WorkingDir  string             `json:"working_dir"`
	SessionID   string             `json:"session_id"`
	CallID      string             `json:"call_id"`
	Origin      string             `json:"origin"`
	ClientID    string             `json:"client_id"`
	Mode        string             `json:"mode"`
	AgentName   string             `json:"agent_name"`
	CommandName string             `json:"command_name"`
	CommandArgs string             `json:"command_args"`
	Status      string             `json:"status"`
	Result      string             `json:"result"`
	Metadata    string             `json:"metadata"`
	Error       string             `json:"error"`
	CreatedAt   string             `json:"created_at"`
	UpdatedAt   string             `json:"updated_at"`
	CompletedAt string             `json:"completed_at"`
	Progress    []wireToolProgress `json:"progress"`
}

type wireToolProgress struct {
	Timestamp string `json:"timestamp"`
	Text      string `json:"text"`
	Metadata  string `json:"metadata"`
	Status    string `json:"status"`
}

const requestWatchToolTask = "tool_watch"

func WatchAsyncTask(ctx context.Context, taskID string) (<-chan AsyncTaskEvent, func(), error) {
	return WatchAsyncTaskOnDaemon(ctx, taskID, "local")
}

func WatchAsyncTaskOnDaemon(ctx context.Context, taskID, daemonName string) (<-chan AsyncTaskEvent, func(), error) {
	trimmed := strings.TrimSpace(taskID)
	if trimmed == "" {
		return nil, nil, fmt.Errorf("task id is required")
	}
	if daemonName == "" {
		daemonName = "local"
	}
	payload := map[string]any{"type": requestWatchToolTask, "task_id": trimmed}
	conn, cleanup, err := openStreamToDaemon(ctx, daemonName, payload)
	if err != nil {
		return nil, nil, err
	}
	scanner := bufio.NewScanner(conn)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 64*1024*1024)
	if !scanner.Scan() {
		cleanup()
		if err := scanner.Err(); err != nil {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("stream closed")
	}
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("decode stream ack: %w", err)
	}
	if !resp.Success {
		cleanup()
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "stream rejected"
		}
		return nil, nil, fmt.Errorf(errMsg)
	}
	events := make(chan AsyncTaskEvent, 32)
	var cancelOnce sync.Once
	cancel := func() {
		cancelOnce.Do(func() {
			cleanup()
		})
	}
	go func(sc *bufio.Scanner) {
		defer close(events)
		defer cancel()
		for sc.Scan() {
			line := sc.Bytes()
			var payload wireToolTaskEvent
			if err := json.Unmarshal(line, &payload); err != nil {
				continue
			}
			ev, err := convertAsyncTaskEvent(payload)
			if err != nil {
				continue
			}
			select {
			case events <- ev:
			default:
			}
		}
	}(scanner)
	return events, cancel, nil
}

func convertAsyncTaskEvent(ev wireToolTaskEvent) (AsyncTaskEvent, error) {
	result := AsyncTaskEvent{Type: AsyncTaskEventType(strings.TrimSpace(ev.Type)), Error: strings.TrimSpace(ev.Error)}
	if ev.Task != nil {
		task, err := asyncTaskFromWireTask(ev.Task)
		if err != nil {
			return result, err
		}
		result.Task = &task
	}
	if ev.Progress != nil {
		timestamp := time.Time{}
		if trimmed := strings.TrimSpace(ev.Progress.Timestamp); trimmed != "" {
			if ts, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
				timestamp = ts
			}
		}
		result.Progress = &AsyncTaskProgress{
			Timestamp: timestamp,
			Text:      strings.TrimSpace(ev.Progress.Text),
			Metadata:  strings.TrimSpace(ev.Progress.Metadata),
			Status:    strings.TrimSpace(ev.Progress.Status),
		}
	}
	return result, nil
}

func asyncTaskFromWireTask(task *wireToolTask) (AsyncTask, error) {
	if task == nil {
		return AsyncTask{}, fmt.Errorf("nil task")
	}
	raw := rawAsyncTask{
		ID:          task.ID,
		ToolName:    task.ToolName,
		Args:        task.Args,
		WorkingDir:  task.WorkingDir,
		SessionID:   task.SessionID,
		CallID:      task.CallID,
		Origin:      task.Origin,
		ClientID:    task.ClientID,
		Status:      task.Status,
		Result:      task.Result,
		Metadata:    task.Metadata,
		Error:       task.Error,
		CreatedAt:   task.CreatedAt,
		UpdatedAt:   task.UpdatedAt,
		CompletedAt: task.CompletedAt,
		Mode:        task.Mode,
		AgentName:   task.AgentName,
		CommandName: task.CommandName,
		CommandArgs: task.CommandArgs,
	}
	if len(task.Progress) > 0 {
		raw.Progress = make([]rawAsyncProgress, 0, len(task.Progress))
		for _, entry := range task.Progress {
			raw.Progress = append(raw.Progress, rawAsyncProgress{
				Timestamp: entry.Timestamp,
				Text:      entry.Text,
				Metadata:  entry.Metadata,
				Status:    entry.Status,
			})
		}
	}
	return decodeAsyncTask(raw)
}
