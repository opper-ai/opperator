package lsp

import (
	"context"
	"encoding/json"
	"log/slog"

	"tui/lsp/util"

	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// HandleWorkspaceConfiguration responds to workspace/configuration requests with an empty payload.
func HandleWorkspaceConfiguration(_ context.Context, _ string, _ json.RawMessage) (any, error) {
	return []map[string]any{{}}, nil
}

func HandleRegisterCapability(_ context.Context, _ string, params json.RawMessage) (any, error) {
	var registerParams protocol.RegistrationParams
	if err := json.Unmarshal(params, &registerParams); err != nil {
		slog.Error("lsp: decode registration params", "error", err)
		return nil, err
	}

	for _, reg := range registerParams.Registrations {
		if reg.Method != "workspace/didChangeWatchedFiles" {
			continue
		}
		optionsJSON, err := json.Marshal(reg.RegisterOptions)
		if err != nil {
			slog.Error("lsp: marshal registration options", "error", err)
			continue
		}
		var options protocol.DidChangeWatchedFilesRegistrationOptions
		if err := json.Unmarshal(optionsJSON, &options); err != nil {
			slog.Error("lsp: decode watch registration", "error", err)
			continue
		}
		notifyFileWatchRegistration(reg.ID, options.Watchers)
	}
	return nil, nil
}

// HandleApplyEdit applies workspace edits requested by the server.
func HandleApplyEdit(_ context.Context, _ string, params json.RawMessage) (any, error) {
	var edit protocol.ApplyWorkspaceEditParams
	if err := json.Unmarshal(params, &edit); err != nil {
		return nil, err
	}
	if err := util.ApplyWorkspaceEdit(edit.Edit); err != nil {
		slog.Error("lsp: apply workspace edit", "error", err)
		return protocol.ApplyWorkspaceEditResult{Applied: false, FailureReason: err.Error()}, nil
	}
	return protocol.ApplyWorkspaceEditResult{Applied: true}, nil
}

// FileWatchRegistrationHandler receives file watcher registrations.
type FileWatchRegistrationHandler func(id string, watchers []protocol.FileSystemWatcher)

var fileWatchHandler FileWatchRegistrationHandler

func RegisterFileWatchHandler(handler FileWatchRegistrationHandler) {
	fileWatchHandler = handler
}

func notifyFileWatchRegistration(id string, watchers []protocol.FileSystemWatcher) {
	if fileWatchHandler != nil {
		fileWatchHandler(id, watchers)
	}
}

// HandleServerMessage logs window/showMessage notifications when debugging is enabled.
func HandleServerMessage(debug bool, method string, params json.RawMessage) {
	if !debug {
		return
	}
	if method != "window/showMessage" {
		return
	}
	var msg protocol.ShowMessageParams
	if err := json.Unmarshal(params, &msg); err != nil {
		slog.Debug("lsp server message", "method", method, "payload", string(params))
		return
	}
	switch msg.Type {
	case protocol.Error:
		slog.Error("lsp server", "message", msg.Message)
	case protocol.Warning:
		slog.Warn("lsp server", "message", msg.Message)
	case protocol.Info:
		slog.Info("lsp server", "message", msg.Message)
	case protocol.Log:
		slog.Debug("lsp server", "message", msg.Message)
	}
}

// HandleDiagnostics updates the diagnostics cache and triggers the registered callback.
func HandleDiagnostics(client *Client, params json.RawMessage) {
	var diagParams protocol.PublishDiagnosticsParams
	if err := json.Unmarshal(params, &diagParams); err != nil {
		slog.Error("lsp: decode diagnostics", "error", err)
		return
	}

	client.diagnostics.Set(diagParams.URI, diagParams.Diagnostics)

	total := 0
	for _, diags := range client.diagnostics.Snapshot() {
		total += len(diags)
	}

	if client.onDiagnosticsChanged != nil {
		client.onDiagnosticsChanged(client.name, total)
	}
}
