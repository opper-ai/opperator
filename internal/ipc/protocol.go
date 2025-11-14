package ipc

import (
	"encoding/json"
	"opperator/internal/agent"
	"opperator/internal/protocol"
)

type RequestType string

const (
	RequestListAgents        RequestType = "list"
	RequestStartAgent        RequestType = "start"
	RequestStopAgent         RequestType = "stop"
	RequestRestartAgent      RequestType = "restart"
	RequestStopAll           RequestType = "stop_all"
	RequestGetLogs           RequestType = "get_logs"
	RequestGetCustomSections RequestType = "get_custom_sections"
	RequestReloadConfig      RequestType = "reload_config"
	RequestShutdown          RequestType = "shutdown"
	RequestCommand           RequestType = "command"
	RequestListCommands      RequestType = "list_commands"
	RequestSubmitToolTask    RequestType = "tool_submit"
	RequestGetToolTask       RequestType = "tool_get"
	RequestListToolTasks     RequestType = "tool_list"
	RequestDeleteToolTask    RequestType = "tool_delete"
	RequestWatchToolTask     RequestType = "tool_watch"
	RequestToolTaskMetrics   RequestType = "tool_metrics"
	RequestGetSecret         RequestType = "secret_get"
	RequestSetSecret         RequestType = "secret_set"
	RequestDeleteSecret      RequestType = "secret_delete"
	RequestListSecrets       RequestType = "secret_list"
	RequestWatchAgentState   RequestType = "watch_agent_state"
	RequestWatchAllTasks     RequestType = "watch_all_tasks"
	RequestLifecycleEvent    RequestType = "lifecycle_event"
	RequestGetAgentConfig    RequestType = "get_agent_config"
	RequestBootstrapAgent    RequestType = "bootstrap_agent"
	RequestDeleteAgent       RequestType = "delete_agent"
	RequestReceiveAgent      RequestType = "receive_agent"
	RequestPackageAgent      RequestType = "package_agent"
	RequestSetInvocationDir  RequestType = "set_invocation_dir"
	RequestGetInvocationDir  RequestType = "get_invocation_dir"
)

type Request struct {
	Type          RequestType            `json:"type"`
	AgentName     string                 `json:"agent_name,omitempty"`
	Command       string                 `json:"command,omitempty"`
	Args          map[string]interface{} `json:"args,omitempty"`
	ToolName      string                 `json:"tool_name,omitempty"`
	ToolArgs      string                 `json:"tool_args,omitempty"`
	TaskID        string                 `json:"task_id,omitempty"`
	WorkingDir    string                 `json:"working_dir,omitempty"`
	SessionID     string                 `json:"session_id,omitempty"`
	CallID        string                 `json:"call_id,omitempty"`
	Mode          string                 `json:"mode,omitempty"`
	CommandArgs   string                 `json:"command_args,omitempty"`
	Origin        string                 `json:"origin,omitempty"`
	ClientID      string                 `json:"client_id,omitempty"`
	SecretName    string                 `json:"secret_name,omitempty"`
	SecretValue   string                 `json:"secret_value,omitempty"`
	LifecycleType string                 `json:"lifecycle_type,omitempty"`
	LifecycleData map[string]interface{} `json:"lifecycle_data,omitempty"`
	Description   string                 `json:"description,omitempty"`
	NoStart       bool                   `json:"no_start,omitempty"`

	// Agent transfer fields
	AgentPackage *agent.AgentPackage `json:"agent_package,omitempty"`
	Force        bool                `json:"force,omitempty"`
	StartAfter   bool                `json:"start_after,omitempty"`
}

type Response struct {
	Success       bool                              `json:"success"`
	Error         string                            `json:"error,omitempty"`
	Processes     []*ProcessInfo                    `json:"processes,omitempty"`
	Logs          []string                          `json:"logs,omitempty"`
	Command       *CommandResponse                  `json:"command,omitempty"`
	Commands      []protocol.CommandDescriptor      `json:"commands,omitempty"`
	Progress      *protocol.CommandProgressMessage  `json:"progress,omitempty"`
	Task          *ToolTask                         `json:"task,omitempty"`
	Tasks         []*ToolTask                       `json:"tasks,omitempty"`
	Secret        string                            `json:"secret,omitempty"`
	Secrets       []string                          `json:"secrets,omitempty"`
	Metrics       *ToolTaskMetrics                  `json:"metrics,omitempty"`
	Sections      interface{}                       `json:"sections,omitempty"`
	ProcessRoot   string                            `json:"process_root,omitempty"`
	AgentPackage  *agent.AgentPackage               `json:"agent_package,omitempty"`
	InvocationDir string                            `json:"invocation_dir,omitempty"`
}

type ToolTaskMetrics struct {
	Submitted   int64 `json:"submitted"`
	InFlight    int64 `json:"in_flight"`
	Completed   int64 `json:"completed"`
	Failed      int64 `json:"failed"`
	QueueDepth  int64 `json:"queue_depth"`
	WorkerCount int64 `json:"worker_count"`
}

type ToolTaskEvent struct {
	Type     string            `json:"type"`
	Task     *ToolTask         `json:"task,omitempty"`
	Progress *ToolTaskProgress `json:"progress,omitempty"`
	Error    string            `json:"error,omitempty"`
}

type AgentStateEvent struct {
	Type                string                       `json:"type"` // "metadata", "logs", "sections", "status"
	AgentName           string                       `json:"agent_name"`
	Description         string                       `json:"description,omitempty"`
	SystemPrompt        string                       `json:"system_prompt,omitempty"`
	SystemPromptReplace bool                         `json:"system_prompt_replace,omitempty"`
	Color               string                       `json:"color,omitempty"`
	Logs                []string                     `json:"logs,omitempty"`      // For bulk log updates (initial load)
	LogEntry            string                       `json:"log_entry,omitempty"` // For single log append events
	CustomSections      interface{}                  `json:"custom_sections,omitempty"`
	Status              string                       `json:"status,omitempty"`
	Commands            []protocol.CommandDescriptor `json:"commands,omitempty"`
}

type CommandResponse struct {
	Success bool        `json:"success"`
	Error   string      `json:"error,omitempty"`
	Result  interface{} `json:"result,omitempty"`
}

type ToolTask struct {
	ID          string             `json:"id"`
	ToolName    string             `json:"tool_name"`
	Args        string             `json:"args"`
	WorkingDir  string             `json:"working_dir"`
	SessionID   string             `json:"session_id,omitempty"`
	CallID      string             `json:"call_id,omitempty"`
	Mode        string             `json:"mode,omitempty"`
	AgentName   string             `json:"agent_name,omitempty"`
	CommandName string             `json:"command_name,omitempty"`
	CommandArgs string             `json:"command_args,omitempty"`
	Origin      string             `json:"origin,omitempty"`
	ClientID    string             `json:"client_id,omitempty"`
	Status      string             `json:"status"`
	Result      string             `json:"result,omitempty"`
	Metadata    string             `json:"metadata,omitempty"`
	Error       string             `json:"error,omitempty"`
	CreatedAt   string             `json:"created_at"`
	UpdatedAt   string             `json:"updated_at"`
	CompletedAt string             `json:"completed_at,omitempty"`
	Progress    []ToolTaskProgress `json:"progress,omitempty"`
}

type ToolTaskProgress struct {
	Timestamp string `json:"timestamp"`
	Text      string `json:"text,omitempty"`
	Metadata  string `json:"metadata,omitempty"`
	Status    string `json:"status,omitempty"`
}

type ProcessInfo struct {
	Name                string              `json:"name"`
	Description         string              `json:"description,omitempty"`
	Status              agent.ProcessStatus `json:"status"`
	PID                 int                 `json:"pid"`
	RestartCount        int                 `json:"restart_count"`
	Uptime              int64               `json:"uptime"` // seconds
	SystemPrompt        string              `json:"system_prompt,omitempty"`
	SystemPromptReplace bool                `json:"system_prompt_replace,omitempty"`
	Color               string              `json:"color,omitempty"`
}

func EncodeRequest(req Request) ([]byte, error) {
	return json.Marshal(req)
}

func DecodeRequest(data []byte) (Request, error) {
	var req Request
	err := json.Unmarshal(data, &req)
	return req, err
}

func EncodeResponse(resp Response) ([]byte, error) {
	return json.Marshal(resp)
}

func DecodeResponse(data []byte) (Response, error) {
	var resp Response
	err := json.Unmarshal(data, &resp)
	return resp, err
}
