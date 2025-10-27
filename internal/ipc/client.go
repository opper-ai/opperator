package ipc

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"opperator/internal/protocol"
)

type Client struct {
	conn net.Conn
}

func NewClient(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}

	return &Client{conn: conn}, nil
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

//
