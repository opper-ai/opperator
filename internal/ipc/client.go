package ipc

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"opperator/config"
	"opperator/internal/agent"
	"opperator/internal/protocol"
)

type Client struct {
	conn net.Conn
}

// NewClient creates a new IPC client that can connect via Unix socket or TCP
// address formats:
//   - unix:///path/to/socket.sock
//   - tcp://hostname:port
//   - /path/to/socket.sock (legacy, assumes unix socket)
func NewClient(address string) (*Client, error) {
	return NewClientWithAuth(address, "")
}

// NewClientWithAuth creates a new IPC client with optional authentication
func NewClientWithAuth(address, authToken string) (*Client, error) {
	var network, addr string

	// Parse address scheme
	if strings.HasPrefix(address, "unix://") {
		network = "unix"
		addr = strings.TrimPrefix(address, "unix://")
	} else if strings.HasPrefix(address, "tcp://") {
		network = "tcp"
		addr = strings.TrimPrefix(address, "tcp://")
	} else {
		// Legacy format - assume unix socket
		network = "unix"
		addr = address
	}

	// Establish connection
	conn, err := net.DialTimeout(network, addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}

	// For TCP connections, perform authentication handshake
	if network == "tcp" {
		if err := performAuthHandshake(conn, authToken); err != nil {
			conn.Close()
			return nil, fmt.Errorf("authentication failed: %w", err)
		}
	}

	return &Client{conn: conn}, nil
}

// performAuthHandshake sends the auth token and waits for confirmation
func performAuthHandshake(conn net.Conn, token string) error {
	// Set timeout for auth handshake
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetDeadline(time.Time{})

	// Send auth token
	authMsg := fmt.Sprintf("AUTH %s\n", token)
	if _, err := conn.Write([]byte(authMsg)); err != nil {
		return fmt.Errorf("failed to send auth token: %w", err)
	}

	// Read response
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("failed to read auth response: %w", err)
		}
		return fmt.Errorf("no auth response from server")
	}

	response := strings.TrimSpace(scanner.Text())
	if response != "OK" {
		return fmt.Errorf("auth rejected: %s", response)
	}

	return nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) sendRequest(req Request) (Response, error) {
	return c.sendRequestWithTimeout(req, 10*time.Second)
}

func (c *Client) sendRequestWithTimeout(req Request, timeout time.Duration) (Response, error) {
	data, err := EncodeRequest(req)
	if err != nil {
		return Response{}, err
	}

	c.conn.SetWriteDeadline(time.Now().Add(timeout))
	_, err = c.conn.Write(append(data, '\n'))
	if err != nil {
		return Response{}, fmt.Errorf("write timeout: %w", err)
	}

	c.conn.SetReadDeadline(time.Now().Add(timeout))
	scanner := bufio.NewScanner(c.conn)

	// Increase buffer size to handle large log lines (default is 64KB, increase to 1MB)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return Response{}, fmt.Errorf("read timeout: %w", err)
		}
		return Response{}, fmt.Errorf("no response from daemon")
	}

	c.conn.SetDeadline(time.Time{})

	return DecodeResponse(scanner.Bytes())
}

func (c *Client) ListAgents() ([]*ProcessInfo, error) {
	req := Request{Type: RequestListAgents}
	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}

	return resp.Processes, nil
}

func (c *Client) StartAgent(name string) error {
	req := Request{Type: RequestStartAgent, AgentName: name}
	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	return nil
}

func (c *Client) StopAgent(name string) error {
	req := Request{Type: RequestStopAgent, AgentName: name}
	resp, err := c.sendRequestWithTimeout(req, 15*time.Second)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	return nil
}

func (c *Client) RestartAgent(name string) error {
	req := Request{Type: RequestRestartAgent, AgentName: name}
	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	return nil
}

func (c *Client) InvokeCommand(name, command string, args map[string]interface{}, timeout time.Duration) (*CommandResponse, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	req := Request{Type: RequestCommand, AgentName: name, Command: command, Args: args}
	if cwd, err := os.Getwd(); err == nil {
		if abs, absErr := filepath.Abs(cwd); absErr == nil {
			cwd = abs
		}
		req.WorkingDir = filepath.Clean(cwd)
	}
	resp, err := c.sendRequestWithTimeout(req, timeout)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}

	if resp.Command == nil {
		return nil, fmt.Errorf("missing command response")
	}

	return resp.Command, nil
}

func (c *Client) ListCommands(name string) ([]protocol.CommandDescriptor, error) {
	req := Request{Type: RequestListCommands, AgentName: name}
	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}

	return resp.Commands, nil
}

func (c *Client) StopAll() error {
	req := Request{Type: RequestStopAll}
	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	return nil
}

func (c *Client) GetLogs(name string) ([]string, error) {
	req := Request{Type: RequestGetLogs, AgentName: name}
	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}

	return resp.Logs, nil
}

func (c *Client) ToolTaskMetrics() (ToolTaskMetrics, error) {
	req := Request{Type: RequestToolTaskMetrics}
	resp, err := c.sendRequest(req)
	if err != nil {
		return ToolTaskMetrics{}, err
	}
	if !resp.Success {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "failed to fetch metrics"
		}
		return ToolTaskMetrics{}, fmt.Errorf("%s", errMsg)
	}
	if resp.Metrics == nil {
		return ToolTaskMetrics{}, fmt.Errorf("daemon did not return metrics")
	}
	return *resp.Metrics, nil
}

func (c *Client) ListToolTasks() ([]*ToolTask, error) {
	req := Request{Type: RequestListToolTasks}
	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "failed to list tasks"
		}
		return nil, fmt.Errorf("%s", errMsg)
	}
	return resp.Tasks, nil
}

func (c *Client) GetToolTask(id string) (*ToolTask, error) {
	req := Request{Type: RequestGetToolTask, TaskID: strings.TrimSpace(id)}
	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "task not found"
		}
		return nil, fmt.Errorf("%s", errMsg)
	}
	if resp.Task == nil {
		return nil, fmt.Errorf("daemon returned no task payload")
	}
	return resp.Task, nil
}

func (c *Client) DeleteToolTask(id string) error {
	req := Request{Type: RequestDeleteToolTask, TaskID: strings.TrimSpace(id)}
	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}
	if !resp.Success {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "failed to delete task"
		}
		return fmt.Errorf("%s", errMsg)
	}
	return nil
}

func (c *Client) Shutdown() error {
	req := Request{Type: RequestShutdown}
	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	return nil
}

func (c *Client) GetSecret(name string) (string, error) {
	req := Request{Type: RequestGetSecret, SecretName: name}
	resp, err := c.sendRequest(req)
	if err != nil {
		return "", err
	}

	if !resp.Success {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "failed to retrieve secret"
		}
		return "", fmt.Errorf("%s", errMsg)
	}

	return resp.Secret, nil
}

func (c *Client) ReloadConfig() error {
	req := Request{Type: RequestReloadConfig}
	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	return nil
}

func (c *Client) BootstrapAgent(name, description string, noStart bool) (string, error) {
	req := Request{
		Type:        RequestBootstrapAgent,
		AgentName:   name,
		Description: description,
		NoStart:     noStart,
	}
	// Bootstrap can take longer, so use a longer timeout
	resp, err := c.sendRequestWithTimeout(req, 60*time.Second)
	if err != nil {
		return "", err
	}

	if !resp.Success {
		return "", fmt.Errorf("%s", resp.Error)
	}

	// The daemon returns the success message in the Error field for backwards compatibility
	return resp.Error, nil
}

func (c *Client) DeleteAgent(name string) error {
	req := Request{
		Type:      RequestDeleteAgent,
		AgentName: name,
	}
	resp, err := c.sendRequestWithTimeout(req, 30*time.Second)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	return nil
}

func (c *Client) ReceiveAgent(pkg *agent.AgentPackage, force, startAfter bool) error {
	req := Request{
		Type:         RequestReceiveAgent,
		AgentPackage: pkg,
		Force:        force,
		StartAfter:   startAfter,
	}
	resp, err := c.sendRequestWithTimeout(req, 60*time.Second) // Longer timeout for file transfer
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}

	return nil
}

func (c *Client) PackageAgent(name string) (*agent.AgentPackage, error) {
	req := Request{
		Type:      RequestPackageAgent,
		AgentName: name,
	}
	resp, err := c.sendRequestWithTimeout(req, 60*time.Second) // Longer timeout for file transfer
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}

	if resp.AgentPackage == nil {
		return nil, fmt.Errorf("no agent package returned")
	}

	return resp.AgentPackage, nil
}

// NewClientFromRegistry creates a client using daemon configuration from the registry
func NewClientFromRegistry(daemonName string) (*Client, error) {
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to load daemon registry: %w", err)
	}

	daemon, err := registry.GetDaemon(daemonName)
	if err != nil {
		return nil, err
	}

	if !daemon.Enabled {
		return nil, fmt.Errorf("daemon '%s' is disabled", daemonName)
	}

	return NewClientWithAuth(daemon.Address, daemon.AuthToken)
}

//
