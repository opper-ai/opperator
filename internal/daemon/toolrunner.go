package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"opperator/internal/agent"
	"opperator/internal/protocol"
	"opperator/internal/taskqueue"

	tooling "tui/tools"
)

// daemonToolRunner adapts the existing tool implementations for use within the
// daemon-managed asynchronous task queue.
type daemonToolRunner struct{}

func newDaemonToolRunner() *daemonToolRunner {
	return &daemonToolRunner{}
}

func (r *daemonToolRunner) Execute(ctx context.Context, name, args, workingDir string) (string, string, error) {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch lower {
	case tooling.ViewToolName:
		content, metadata := tooling.RunView(ctx, args, workingDir)
		return content, metadata, nil
	case tooling.LSToolName:
		content, metadata := tooling.RunLS(ctx, args, workingDir)
		return content, metadata, nil
	case tooling.WriteToolName:
		content, metadata := tooling.RunWrite(ctx, args, workingDir)
		return content, metadata, nil
	case tooling.EditToolName:
		content, metadata := tooling.RunEdit(ctx, args, workingDir)
		return content, metadata, nil
	case tooling.MultiEditToolName:
		content, metadata := tooling.RunMultiEdit(ctx, args, workingDir)
		return content, metadata, nil
	case tooling.GlobToolName:
		content, metadata := tooling.RunGlob(ctx, args, workingDir)
		return content, metadata, nil
	case tooling.GrepToolName:
		content, metadata := tooling.RunGrep(ctx, args, workingDir)
		return content, metadata, nil
	case tooling.RGToolName:
		content, metadata := tooling.RunRG(ctx, args, workingDir)
		return content, metadata, nil
	case tooling.BashToolName:
		content, metadata := tooling.RunBash(ctx, args, workingDir)
		return content, metadata, nil
	case tooling.ListAgentsToolName:
		content, metadata := tooling.RunListAgents(ctx, args)
		return content, metadata, nil
	case tooling.StartAgentToolName:
		content, metadata := tooling.RunStartAgent(ctx, args)
		return content, metadata, nil
	case tooling.StopAgentToolName:
		content, metadata := tooling.RunStopAgent(ctx, args)
		return content, metadata, nil
	case tooling.RestartAgentToolName:
		content, metadata := tooling.RunRestartAgent(ctx, args)
		return content, metadata, nil
	case tooling.GetLogsToolName:
		content, metadata := tooling.RunGetLogs(ctx, args)
		return content, metadata, nil
	case tooling.ListSecretsToolName:
		content, metadata := tooling.RunListSecrets(ctx, args)
		return content, metadata, nil
	default:
		return "", "", fmt.Errorf("unsupported async tool: %s", name)
	}
}

type daemonAgentRunner struct {
	manager *agent.Manager
}

func newDaemonAgentRunner(manager *agent.Manager) *daemonAgentRunner {
	return &daemonAgentRunner{manager: manager}
}

func (r *daemonAgentRunner) Execute(ctx context.Context, agentName, command, args, workingDir string, progress func(taskqueue.ProgressEvent)) (string, string, error) {
	if r == nil || r.manager == nil {
		return "", "", fmt.Errorf("agent manager unavailable")
	}
	agentName = strings.TrimSpace(agentName)
	command = strings.TrimSpace(command)
	if agentName == "" || command == "" {
		return "", "", fmt.Errorf("agent and command are required")
	}

	var parsed map[string]any
	if trimmed := strings.TrimSpace(args); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return "", "", fmt.Errorf("decode command args: %w", err)
		}
	}

	cb := func(msg protocol.CommandProgressMessage) {
		if progress == nil {
			return
		}
		meta := ""
		if len(msg.Metadata) > 0 {
			if b, err := json.Marshal(msg.Metadata); err == nil {
				meta = string(b)
			}
		}
		progress(taskqueue.ProgressEvent{
			Text:     strings.TrimSpace(msg.Text),
			Metadata: meta,
			Status:   strings.TrimSpace(msg.Status),
		})
	}

	resp, err := r.manager.InvokeCommandAsync(agentName, command, parsed, workingDir, 0, cb)
	if err != nil {
		return "", "", err
	}
	if resp == nil {
		return "", "", fmt.Errorf("agent returned no response")
	}
	if !resp.Success {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "command failed"
		}
		return "", "", fmt.Errorf("%s", errMsg)
	}

	content := "command succeeded"
	if resp.Result != nil {
		if b, err := json.MarshalIndent(resp.Result, "", "  "); err == nil {
			content = string(b)
		}
	}
	meta := map[string]any{
		"agent":   agentName,
		"command": command,
		"success": true,
		"result":  resp.Result,
	}
	metadata := ""
	if mb, err := json.Marshal(meta); err == nil {
		metadata = string(mb)
	}
	return content, metadata, nil
}
