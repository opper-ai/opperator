package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"tui/permission"
	tooling "tui/tools"
)

func requestToolPermission(
	perms permission.Service,
	workingDir string,
	sessionID string,
	toolCallID string,
	toolName string,
	argsJSON string,
	reason string,
) (bool, string) {
	if perms == nil {
		return true, ""
	}

	lower := strings.ToLower(strings.TrimSpace(toolName))
	reason = strings.TrimSpace(reason)
	displayName := strings.TrimSpace(toolName)

	switch lower {
	case tooling.ViewToolName:
		var params struct {
			FilePath string `json:"file_path"`
			Offset   int    `json:"offset"`
			Limit    int    `json:"limit"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
			return true, ""
		}
		if strings.TrimSpace(params.FilePath) == "" {
			return true, ""
		}
		absPath, err := tooling.ResolveWorkingPath(workingDir, params.FilePath)
		if err != nil {
			return false, err.Error()
		}
		description := fmt.Sprintf("Allow %s to read %s", displayName, absPath)
		if reason != "" {
			description += " — " + reason
		}
		return perms.Request(permission.CreatePermissionRequest{
			SessionID:   sessionID,
			ToolCallID:  toolCallID,
			ToolName:    toolName,
			Description: description,
			Action:      "Read file",
			Params:      params,
			Path:        absPath,
			Reason:      reason,
		}), ""
	case tooling.LSToolName:
		var params struct {
			Path   string   `json:"path"`
			Ignore []string `json:"ignore"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
			return true, ""
		}
		searchPath := params.Path
		if strings.TrimSpace(searchPath) == "" {
			searchPath = workingDir
		}
		absPath, err := tooling.ResolveWorkingPath(workingDir, searchPath)
		if err != nil {
			return false, err.Error()
		}
		description := fmt.Sprintf("Allow %s to list %s", displayName, absPath)
		if reason != "" {
			description += " — " + reason
		}
		return perms.Request(permission.CreatePermissionRequest{
			SessionID:   sessionID,
			ToolCallID:  toolCallID,
			ToolName:    toolName,
			Description: description,
			Action:      "List files",
			Params:      params,
			Path:        absPath,
			Reason:      reason,
		}), ""
	case tooling.WriteToolName:
		var params struct {
			FilePath string `json:"file_path"`
			Content  string `json:"content"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
			return true, ""
		}
		absPath, err := tooling.ResolveWorkingPath(workingDir, params.FilePath)
		if err != nil {
			return false, err.Error()
		}
		description := fmt.Sprintf("Allow %s to write %s", displayName, absPath)
		if reason != "" {
			description += " — " + reason
		}
		return perms.Request(permission.CreatePermissionRequest{
			SessionID:   sessionID,
			ToolCallID:  toolCallID,
			ToolName:    toolName,
			Description: description,
			Action:      "Write file",
			Params:      map[string]any{"file_path": params.FilePath},
			Path:        absPath,
			Reason:      reason,
		}), ""
	case tooling.EditToolName, tooling.MultiEditToolName:
		var params struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
			return true, ""
		}
		absPath, err := tooling.ResolveWorkingPath(workingDir, params.FilePath)
		if err != nil {
			return false, err.Error()
		}
		description := fmt.Sprintf("Allow %s to modify %s", displayName, absPath)
		if reason != "" {
			description += " — " + reason
		}
		return perms.Request(permission.CreatePermissionRequest{
			SessionID:   sessionID,
			ToolCallID:  toolCallID,
			ToolName:    toolName,
			Description: description,
			Action:      "Modify file",
			Params:      map[string]any{"file_path": params.FilePath},
			Path:        absPath,
			Reason:      reason,
		}), ""
	case tooling.BashToolName:
		var params struct {
			Command string `json:"command"`
			Timeout int    `json:"timeout"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
			return true, ""
		}
		description := fmt.Sprintf("Allow bash command: %s", truncateForPermission(params.Command))
		if reason != "" {
			description += " — " + reason
		}
		return perms.Request(permission.CreatePermissionRequest{
			SessionID:   sessionID,
			ToolCallID:  toolCallID,
			ToolName:    toolName,
			Description: description,
			Action:      "Run command",
			Params:      params,
			Path:        workingDir,
			Reason:      reason,
		}), ""
	default:
		return true, ""
	}
}
