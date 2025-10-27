package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

func dialIPC(ctx context.Context) (net.Conn, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sock := filepath.Join(os.TempDir(), "opperator.sock")
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", sock)
	if err != nil {
		return nil, nil, err
	}
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
	conn, cleanup, err := dialIPC(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := writePayload(ctx, conn, payload); err != nil {
		cleanup()
		return nil, nil, err
	}
	return conn, cleanup, nil
}

// OpenStream exposes the stream opening functionality for external callers.
func OpenStream(ctx context.Context, payload any) (net.Conn, func(), error) {
	return openStream(ctx, payload)
}

func ipcRequestCtx(ctx context.Context, payload any) ([]byte, error) {
	conn, cleanup, err := dialIPC(ctx)
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

// IPCRequestCtx exposes the daemon IPC helper for external callers.
func IPCRequestCtx(ctx context.Context, payload any) ([]byte, error) {
	return ipcRequestCtx(ctx, payload)
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
