package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"opperator/config"
)

// dialIPC connects to the local daemon (backward compatibility)
func dialIPC(ctx context.Context) (net.Conn, func(), error) {
	return dialIPCDaemon(ctx, "local")
}

// dialIPCDaemon connects to a specific daemon by name from the registry
func dialIPCDaemon(ctx context.Context, daemonName string) (net.Conn, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Load daemon registry
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return nil, nil, fmt.Errorf("load daemon registry: %w", err)
	}

	// Get daemon config
	daemon, err := registry.GetDaemon(daemonName)
	if err != nil {
		return nil, nil, fmt.Errorf("get daemon '%s': %w", daemonName, err)
	}

	// Parse address
	var network, addr string
	if strings.HasPrefix(daemon.Address, "unix://") {
		network = "unix"
		addr = strings.TrimPrefix(daemon.Address, "unix://")
	} else if strings.HasPrefix(daemon.Address, "tcp://") {
		network = "tcp"
		addr = strings.TrimPrefix(daemon.Address, "tcp://")
	} else {
		return nil, nil, fmt.Errorf("invalid daemon address: %s", daemon.Address)
	}

	// Dial with timeout - respect context deadline if set, otherwise use default
	timeout := 5 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, nil, err
	}

	// For TCP connections, perform authentication
	if network == "tcp" && daemon.AuthToken != "" {
		if err := performAuthHandshake(conn, daemon.AuthToken); err != nil {
			conn.Close()
			return nil, nil, fmt.Errorf("auth failed: %w", err)
		}
	}

	// Setup cleanup
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	cleanup := func() {
		close(done)
		_ = conn.Close()
	}

	return conn, cleanup, nil
}

// performAuthHandshake handles TCP authentication
func performAuthHandshake(conn net.Conn, token string) error {
	// Send auth message
	authMsg := fmt.Sprintf("AUTH %s\n", token)
	if _, err := conn.Write([]byte(authMsg)); err != nil {
		return err
	}

	// Read response
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return err
		}
		return fmt.Errorf("no auth response")
	}

	response := strings.TrimSpace(scanner.Text())
	if response != "OK" {
		return fmt.Errorf("auth rejected: %s", response)
	}

	return nil
}

func writePayload(ctx context.Context, conn net.Conn, payload any) error {
	b, _ := json.Marshal(payload)
	if _, err := conn.Write(append(b, '\n')); err != nil {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

func openStream(ctx context.Context, payload any) (net.Conn, func(), error) {
	return openStreamToDaemon(ctx, "local", payload)
}

// openStreamToDaemon opens a stream to a specific daemon
func openStreamToDaemon(ctx context.Context, daemonName string, payload any) (net.Conn, func(), error) {
	conn, cleanup, err := dialIPCDaemon(ctx, daemonName)
	if err != nil {
		return nil, nil, err
	}
	if err := writePayload(ctx, conn, payload); err != nil {
		cleanup()
		return nil, nil, err
	}
	return conn, cleanup, nil
}

// OpenStream exposes the stream opening functionality for external callers (local daemon).
func OpenStream(ctx context.Context, payload any) (net.Conn, func(), error) {
	return openStream(ctx, payload)
}

// OpenStreamToDaemon exposes daemon-specific stream opening for external callers.
func OpenStreamToDaemon(ctx context.Context, daemonName string, payload any) (net.Conn, func(), error) {
	return openStreamToDaemon(ctx, daemonName, payload)
}

func ipcRequestCtx(ctx context.Context, payload any) ([]byte, error) {
	return ipcRequestToDaemon(ctx, "local", payload)
}

// ipcRequestToDaemon sends a request to a specific daemon and returns the response
func ipcRequestToDaemon(ctx context.Context, daemonName string, payload any) ([]byte, error) {
	conn, cleanup, err := dialIPCDaemon(ctx, daemonName)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if err := writePayload(ctx, conn, payload); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(conn)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 64*1024*1024)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			if ctx != nil && ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("no response from daemon")
	}
	return scanner.Bytes(), nil
}

// IPCRequestCtx exposes the daemon IPC helper for external callers (local daemon).
func IPCRequestCtx(ctx context.Context, payload any) ([]byte, error) {
	return ipcRequestCtx(ctx, payload)
}

// IPCRequestToDaemon exposes daemon-specific IPC requests for external callers.
func IPCRequestToDaemon(ctx context.Context, daemonName string, payload any) ([]byte, error) {
	return ipcRequestToDaemon(ctx, daemonName, payload)
}

// SendLifecycleEvent sends a lifecycle event to an agent via the daemon.
func SendLifecycleEvent(agentName, eventType string, data map[string]interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	payload := map[string]interface{}{
		"type":           "lifecycle_event",
		"agent_name":     agentName,
		"lifecycle_type": eventType,
		"lifecycle_data": data,
	}

	// Fire and forget - don't block on errors
	_, _ = ipcRequestCtx(ctx, payload)
}

// FindAgentDaemon searches all enabled daemons to find which one has the specified agent
// Returns the daemon name, or error if not found or ambiguous
func FindAgentDaemon(ctx context.Context, agentName string) (string, error) {
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		// Fallback to local daemon
		return "local", nil
	}

	var foundDaemons []string

	for _, daemon := range registry.Daemons {
		if !daemon.Enabled {
			continue
		}

		// List agents from this daemon
		listPayload := struct {
			Type string `json:"type"`
		}{Type: "list"}

		data, err := ipcRequestToDaemon(ctx, daemon.Name, listPayload)
		if err != nil {
			// Skip daemon if unreachable
			continue
		}

		var listResp struct {
			Success   bool `json:"success"`
			Processes []struct {
				Name string `json:"name"`
			} `json:"processes"`
		}
		if err := json.Unmarshal(data, &listResp); err != nil || !listResp.Success {
			continue
		}

		// Check if agent exists on this daemon
		for _, p := range listResp.Processes {
			if p.Name == agentName {
				foundDaemons = append(foundDaemons, daemon.Name)
				break
			}
		}
	}

	if len(foundDaemons) == 0 {
		return "", fmt.Errorf("agent %q not found on any daemon", agentName)
	}

	if len(foundDaemons) > 1 {
		return "", fmt.Errorf("agent %q exists on multiple daemons: %v", agentName, foundDaemons)
	}

	return foundDaemons[0], nil
}
