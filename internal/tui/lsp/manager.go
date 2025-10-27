package lsp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"tui/internal/csync"
)

// Manager coordinates configured language servers and exposes a simple API for
// triggering diagnostics or responding to file changes.
type Manager struct {
	workspace string
	configs   Configs
	clients   *csync.Map[string, *Client]
	debug     bool

	mu sync.Mutex
}

// NewManager constructs a manager for the provided workspace. Configs may be
// nil which results in an instance without any active language servers.
func NewManager(workspace string, configs Configs, debug bool) *Manager {
	cleaned := strings.TrimSpace(workspace)
	if cleaned == "" {
		cleaned = "."
	}
	if !filepath.IsAbs(cleaned) {
		if abs, err := filepath.Abs(cleaned); err == nil {
			cleaned = abs
		}
	}
	return &Manager{
		workspace: cleaned,
		configs:   configs.Clone(),
		clients:   csync.NewMap[string, *Client](),
		debug:     debug,
	}
}

func (m *Manager) Workspace() string {
	return m.workspace
}

// ClientsSnapshot provides a copy of the currently running LSP clients.
func (m *Manager) ClientsSnapshot() map[string]*Client {
	return m.clients.Snapshot()
}

// Close gracefully stops every running LSP client.
func (m *Manager) Close(ctx context.Context) {
	m.clients.Range(func(name string, client *Client) bool {
		if client == nil {
			return true
		}
		if err := client.Close(ctx); err != nil && m.debug {
			slog.Warn("failed to close LSP client", "name", name, "error", err)
		}
		return true
	})
}

// ensureClient starts the named client if necessary and returns the running instance.
func (m *Manager) ensureClient(ctx context.Context, name string, cfg Config) (*Client, error) {
	if cfg.Disabled {
		return nil, nil
	}
	if existing, ok := m.clients.Get(name); ok && existing != nil {
		return existing, nil
	}

	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("lsp %s has no command configured", name)
	}
	if _, err := exec.LookPath(cfg.Command); err != nil {
		return nil, fmt.Errorf("lsp %s command %q not found: %w", name, cfg.Command, err)
	}
	if !HasRootMarkers(m.workspace, cfg.RootMarkers) {
		return nil, fmt.Errorf("lsp %s skipped: no root markers matched", name)
	}

	client, err := New(ctx, name, cfg, m.workspace, m.debug)
	if err != nil {
		return nil, fmt.Errorf("create lsp client: %w", err)
	}
	client.SetDiagnosticsCallback(func(_ string, _ int) {})

	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if _, err := client.Initialize(initCtx); err != nil {
		client.Close(context.Background())
		return nil, fmt.Errorf("initialize lsp %s: %w", name, err)
	}
	if err := client.WaitForServerReady(initCtx); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) && m.debug {
			slog.Warn("lsp server did not report ready", "name", name, "error", err)
		}
	}

	m.clients.Set(name, client)
	return client, nil
}

// provided file path. If the file path is empty all configured clients are
// considered.
func (m *Manager) clientsForPath(ctx context.Context, path string) ([]*Client, []string) {
	if m == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanPath := strings.TrimSpace(path)
	if cleanPath != "" && !filepath.IsAbs(cleanPath) {
		cleanPath = filepath.Join(m.workspace, cleanPath)
	}
	cleanPath = filepath.Clean(cleanPath)

	type clientResult struct {
		name string
		cfg  Config
	}
	var targets []clientResult
	for name, cfg := range m.configs {
		if cfg.Disabled {
			continue
		}
		if cleanPath == "" || cfg.HandlesFile(cleanPath) {
			targets = append(targets, clientResult{name: name, cfg: cfg})
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].name < targets[j].name })

	clients := make([]*Client, 0, len(targets))
	warnings := make([]string, 0)
	for _, target := range targets {
		client, err := m.ensureClient(ctx, target.name, target.cfg)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		if client != nil {
			clients = append(clients, client)
		}
	}
	return clients, warnings
}

// any warnings encountered during startup.
func (m *Manager) ClientsForPath(ctx context.Context, path string) ([]*Client, []string) {
	return m.clientsForPath(ctx, path)
}

func (m *Manager) Clients(ctx context.Context) ([]*Client, []string) {
	return m.clientsForPath(ctx, "")
}
