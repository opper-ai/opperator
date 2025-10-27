package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"tui/internal/csync"

	powernap "github.com/charmbracelet/x/powernap/pkg/lsp"
	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
	"github.com/charmbracelet/x/powernap/pkg/transport"
)

// Client wraps a powernap LSP client with helpers tailored for the Opperator TUI.
type Client struct {
	client    *powernap.Client
	name      string
	fileTypes []string
	config    Config
	workspace string
	debug     bool

	onDiagnosticsChanged func(name string, count int)
	diagnostics          *csync.VersionedMap[protocol.DocumentURI, []protocol.Diagnostic]
	openFiles            *csync.Map[string, *OpenFileInfo]
	serverState          atomic.Value
}

func New(ctx context.Context, name string, cfg Config, workspace string, debug bool) (*Client, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("lsp %s has no command configured", name)
	}
	root := workspace
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("determine working directory: %w", err)
		}
	}
	if !filepath.IsAbs(root) {
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
	}
	rootURI := string(protocol.URIFromPath(root))

	clientConfig := powernap.ClientConfig{
		Command: cfg.Command,
		Args:    slicesClone(cfg.Args),
		RootURI: rootURI,
		Environment: func() map[string]string {
			env := make(map[string]string)
			maps.Copy(env, cfg.Env)
			return env
		}(),
		Settings:    cfg.Options,
		InitOptions: cfg.InitOptions,
		WorkspaceFolders: []protocol.WorkspaceFolder{
			{
				URI:  rootURI,
				Name: filepath.Base(root),
			},
		},
	}

	powernapClient, err := powernap.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("create powernap client: %w", err)
	}

	c := &Client{
		client:      powernapClient,
		name:        name,
		fileTypes:   slicesClone(cfg.FileTypes),
		config:      cfg.clone(),
		workspace:   root,
		debug:       debug,
		diagnostics: csync.NewVersionedMap[protocol.DocumentURI, []protocol.Diagnostic](),
		openFiles:   csync.NewMap[string, *OpenFileInfo](),
	}
	c.serverState.Store(StateStarting)
	return c, nil
}

type OpenFileInfo struct {
	Version int32
	URI     protocol.DocumentURI
}

func (c *Client) Initialize(ctx context.Context) (*protocol.InitializeResult, error) {
	if err := c.client.Initialize(ctx, false); err != nil {
		return nil, fmt.Errorf("initialize client: %w", err)
	}

	caps := c.client.GetCapabilities()
	protocolCaps := protocol.ServerCapabilities{
		TextDocumentSync: caps.TextDocumentSync,
		CompletionProvider: func() *protocol.CompletionOptions {
			if caps.CompletionProvider != nil {
				return &protocol.CompletionOptions{
					TriggerCharacters:   caps.CompletionProvider.TriggerCharacters,
					AllCommitCharacters: caps.CompletionProvider.AllCommitCharacters,
					ResolveProvider:     caps.CompletionProvider.ResolveProvider,
				}
			}
			return nil
		}(),
	}

	result := &protocol.InitializeResult{Capabilities: protocolCaps}

	c.RegisterServerRequestHandler("workspace/applyEdit", HandleApplyEdit)
	c.RegisterServerRequestHandler("workspace/configuration", HandleWorkspaceConfiguration)
	c.RegisterServerRequestHandler("client/registerCapability", HandleRegisterCapability)
	c.RegisterNotificationHandler("window/showMessage", func(_ context.Context, method string, params json.RawMessage) {
		HandleServerMessage(c.debug, method, params)
	})
	c.RegisterNotificationHandler("textDocument/publishDiagnostics", func(_ context.Context, _ string, params json.RawMessage) {
		HandleDiagnostics(c, params)
	})

	return result, nil
}

// Close shuts down the language server and releases any resources associated with the client.
func (c *Client) Close(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	c.CloseAllFiles(ctx)

	if err := c.client.Shutdown(ctx); err != nil && c.debug {
		slog.Warn("failed to shutdown LSP client", "name", c.name, "error", err)
	}
	return c.client.Exit()
}

type ServerState int

const (
	StateStarting ServerState = iota
	StateReady
	StateError
	StateDisabled
)

func (c *Client) GetServerState() ServerState {
	if v := c.serverState.Load(); v != nil {
		return v.(ServerState)
	}
	return StateStarting
}

func (c *Client) SetServerState(state ServerState) {
	c.serverState.Store(state)
}

func (c *Client) GetName() string {
	return c.name
}

// SetDiagnosticsCallback registers a callback invoked whenever diagnostics change.
func (c *Client) SetDiagnosticsCallback(cb func(name string, count int)) {
	c.onDiagnosticsChanged = cb
}

// WaitForServerReady polls the server until it reports as running or the context expires.
func (c *Client) WaitForServerReady(ctx context.Context) error {
	c.SetServerState(StateStarting)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(30 * time.Second)

	c.openKeyConfigFiles(ctx)

	for {
		select {
		case <-ctx.Done():
			c.SetServerState(StateError)
			return fmt.Errorf("timeout waiting for LSP server to be ready: %w", ctx.Err())
		case <-timeout:
			c.SetServerState(StateError)
			return fmt.Errorf("timeout waiting for LSP server to be ready")
		case <-ticker.C:
			if !c.client.IsRunning() {
				if c.debug {
					slog.Debug("waiting for LSP server", "name", c.name)
				}
				continue
			}
			c.SetServerState(StateReady)
			if c.debug {
				slog.Debug("LSP server ready", "name", c.name)
			}
			return nil
		}
	}
}

// HandlesFile reports whether the client should manage diagnostics for the supplied path.
func (c *Client) HandlesFile(path string) bool {
	if len(c.fileTypes) == 0 {
		return true
	}
	name := strings.ToLower(filepath.Base(path))
	for _, ft := range c.fileTypes {
		suffix := strings.ToLower(strings.TrimSpace(ft))
		if suffix == "" {
			continue
		}
		if !strings.HasPrefix(suffix, ".") {
			suffix = "." + suffix
		}
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// OpenFile notifies the server that a document should be tracked.
func (c *Client) OpenFile(ctx context.Context, path string) error {
	if !c.HandlesFile(path) {
		return nil
	}
	uri := string(protocol.URIFromPath(path))
	if _, exists := c.openFiles.Get(uri); exists {
		return nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	if err := c.client.NotifyDidOpenTextDocument(ctx, uri, string(DetectLanguageID(uri)), 1, string(content)); err != nil {
		return err
	}
	c.openFiles.Set(uri, &OpenFileInfo{Version: 1, URI: protocol.DocumentURI(uri)})
	return nil
}

// OpenFileOnDemand opens the file if it is not already tracked by the server.
func (c *Client) OpenFileOnDemand(ctx context.Context, path string) error {
	if c.IsFileOpen(path) {
		return nil
	}
	return c.OpenFile(ctx, path)
}

// NotifyChange sends the full document content to the language server.
func (c *Client) NotifyChange(ctx context.Context, path string) error {
	uri := string(protocol.URIFromPath(path))
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	info, ok := c.openFiles.Get(uri)
	if !ok {
		return fmt.Errorf("file not opened with LSP: %s", path)
	}
	info.Version++
	changes := []protocol.TextDocumentContentChangeEvent{
		{
			Value: protocol.TextDocumentContentChangeWholeDocument{Text: string(content)},
		},
	}
	return c.client.NotifyDidChangeTextDocument(ctx, uri, int(info.Version), changes)
}

// CloseFile informs the server the document is no longer tracked.
func (c *Client) CloseFile(ctx context.Context, path string) error {
	uri := string(protocol.URIFromPath(path))
	if _, exists := c.openFiles.Get(uri); !exists {
		return nil
	}
	if err := c.client.NotifyDidCloseTextDocument(ctx, uri); err != nil {
		return err
	}
	c.openFiles.Del(uri)
	return nil
}

// CloseAllFiles closes every document currently opened with the server.
func (c *Client) CloseAllFiles(ctx context.Context) {
	snapshot := c.openFiles.Snapshot()
	for uri := range snapshot {
		path, err := protocol.DocumentURI(uri).Path()
		if err != nil {
			continue
		}
		if err := c.CloseFile(ctx, path); err != nil && c.debug {
			slog.Warn("error closing file", "name", c.name, "file", path, "error", err)
		}
	}
}

// IsFileOpen reports whether the language server currently tracks the document.
func (c *Client) IsFileOpen(path string) bool {
	uri := string(protocol.URIFromPath(path))
	_, ok := c.openFiles.Get(uri)
	return ok
}

// GetDiagnostics returns all cached diagnostics grouped by URI.
func (c *Client) GetDiagnostics() map[protocol.DocumentURI][]protocol.Diagnostic {
	return c.diagnostics.Snapshot()
}

func (c *Client) GetFileDiagnostics(uri protocol.DocumentURI) []protocol.Diagnostic {
	diags, _ := c.diagnostics.Get(uri)
	return diags
}

func (c *Client) ClearDiagnosticsForURI(uri protocol.DocumentURI) {
	c.diagnostics.Del(uri)
}

// WaitForDiagnostics blocks until diagnostics are refreshed or the timeout expires.
func (c *Client) WaitForDiagnostics(ctx context.Context, d time.Duration) {
	previous := c.diagnostics.Version()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(d)
	defer timeout.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timeout.C:
			return
		case <-ticker.C:
			if previous != c.diagnostics.Version() {
				return
			}
		}
	}
}

// RegisterNotificationHandler proxies to the underlying powernap client.
func (c *Client) RegisterNotificationHandler(method string, handler transport.NotificationHandler) {
	c.client.RegisterNotificationHandler(method, handler)
}

// RegisterServerRequestHandler proxies to the underlying powernap client.
func (c *Client) RegisterServerRequestHandler(method string, handler transport.Handler) {
	c.client.RegisterHandler(method, handler)
}

// DidChangeWatchedFiles sends a workspace/didChangeWatchedFiles notification to the server.
func (c *Client) DidChangeWatchedFiles(ctx context.Context, params protocol.DidChangeWatchedFilesParams) error {
	return c.client.NotifyDidChangeWatchedFiles(ctx, params.Changes)
}

func (c *Client) openKeyConfigFiles(ctx context.Context) {
	for _, marker := range c.config.RootMarkers {
		candidate := filepath.Join(c.workspace, marker)
		if _, err := os.Stat(candidate); err == nil {
			if err := c.OpenFile(ctx, candidate); err != nil && c.debug {
				slog.Debug("failed to open config file", "file", candidate, "error", err)
			}
		}
	}
}

func slicesClone[T any](in []T) []T {
	if len(in) == 0 {
		return nil
	}
	out := make([]T, len(in))
	copy(out, in)
	return out
}
