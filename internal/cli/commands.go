package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"opperator/internal/config"
	"opperator/internal/ipc"
)

func ListAgents(runningOnly, stoppedOnly, crashedOnly bool) error {
	socketPath, _ := config.GetSocketPath()
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	processes, err := client.ListAgents()
	if err != nil {
		return err
	}

	if len(processes) == 0 {
		fmt.Println("No agents configured")
		return nil
	}

	fmt.Printf("%-20s %-10s %-10s %-8s %s\n", "NAME", "STATUS", "PID", "UPTIME", "DESCRIPTION")
	fmt.Printf("%-20s %-10s %-10s %-8s %s\n", "----", "------", "---", "------", "-----------")

	for _, p := range processes {
		// Apply optional filters
		if runningOnly && string(p.Status) != "running" {
			continue
		}
		if stoppedOnly && string(p.Status) != "stopped" {
			continue
		}
		if crashedOnly && string(p.Status) != "crashed" {
			continue
		}
		status := string(p.Status)
		pid := "-"
		if p.PID > 0 {
			pid = fmt.Sprintf("%d", p.PID)
		}

		uptime := "-"
		if p.Uptime > 0 {
			uptime = fmt.Sprintf("%ds", p.Uptime)
		}

		desc := strings.TrimSpace(p.Description)
		if desc == "" {
			desc = "-"
		}

		fmt.Printf("%-20s %-10s %-10s %-8s %s\n", p.Name, status, pid, uptime, desc)
	}

	return nil
}

func StartAgent(name string) error {
	socketPath, _ := config.GetSocketPath()
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	if err := client.StartAgent(name); err != nil {
		return err
	}
	fmt.Printf("Started agent: %s\n", name)
	return nil
}

func StopAgent(name string) error {
	socketPath, _ := config.GetSocketPath()
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	if err := client.StopAgent(name); err != nil {
		return err
	}
	fmt.Printf("Stopped agent: %s\n", name)
	return nil
}

func RestartAgent(name string) error {
	socketPath, _ := config.GetSocketPath()
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	if err := client.RestartAgent(name); err != nil {
		return err
	}
	fmt.Printf("Restarted agent: %s\n", name)
	return nil
}

func StopAllAgents() error {
	socketPath, _ := config.GetSocketPath()
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	if err := client.StopAll(); err != nil {
		return err
	}
	fmt.Println("Stopped all agents")
	return nil
}

func ReloadConfig() error {
	socketPath, _ := config.GetSocketPath()
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	if err := client.ReloadConfig(); err != nil {
		return err
	}
	fmt.Println("Configuration reloaded successfully")
	return nil
}

func InvokeCommand(name, command string, args map[string]interface{}, timeout time.Duration) error {
	socketPath, _ := config.GetSocketPath()
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	resp, err := client.InvokeCommand(name, command, args, timeout)
	if err != nil {
		return err
	}

	if !resp.Success {
		if resp.Error == "" {
			resp.Error = "command failed"
		}
		return fmt.Errorf("%s", resp.Error)
	}

	fmt.Printf("Command '%s' succeeded on %s\n", command, name)
	if resp.Result != nil {
		if data, err := json.MarshalIndent(resp.Result, "", "  "); err == nil {
			fmt.Println(string(data))
		} else {
			fmt.Printf("Result: %v\n", resp.Result)
		}
	}
	return nil
}

func ListAgentCommands(name string) error {
	socketPath, _ := config.GetSocketPath()
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	commands, err := client.ListCommands(name)
	if err != nil {
		return err
	}

	if len(commands) == 0 {
		fmt.Printf("Agent %s has no registered commands\n", name)
		return nil
	}

	fmt.Printf("Commands for %s:\n", name)
	for _, cmd := range commands {
		displayName := strings.TrimSpace(cmd.Name)
		if displayName == "" {
			continue
		}
		title := strings.TrimSpace(cmd.Title)
		if title != "" && !strings.EqualFold(title, displayName) {
			displayName = fmt.Sprintf("%s (%s)", displayName, title)
		}

		line := fmt.Sprintf("  - %s", displayName)
		if desc := strings.TrimSpace(cmd.Description); desc != "" {
			line += fmt.Sprintf(" — %s", desc)
		}

		if len(cmd.ExposeAs) > 0 {
			exposures := make([]string, 0, len(cmd.ExposeAs))
			for _, exp := range cmd.ExposeAs {
				exposures = append(exposures, string(exp))
			}
			line += fmt.Sprintf(" [%s]", strings.Join(exposures, ", "))
		}

		if slash := strings.TrimSpace(cmd.SlashCommand); slash != "" {
			line += fmt.Sprintf(" (slash: %s)", slash)
		}

		fmt.Println(line)
	}
	return nil
}

func ShowToolTaskMetrics() error {
	socketPath, _ := config.GetSocketPath()
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	metrics, err := client.ToolTaskMetrics()
	if err != nil {
		return err
	}

	fmt.Println("Async Task Queue Metrics")
	fmt.Println("-------------------------")
	fmt.Printf("Submitted:    %d\n", metrics.Submitted)
	fmt.Printf("In Flight:    %d\n", metrics.InFlight)
	fmt.Printf("Completed:    %d\n", metrics.Completed)
	fmt.Printf("Failed:       %d\n", metrics.Failed)
	fmt.Printf("Queue Depth:  %d\n", metrics.QueueDepth)
	fmt.Printf("Worker Count: %d\n", metrics.WorkerCount)

	return nil
}

func GetLogs(name string, follow bool, lines int) error {
	socketPath, _ := config.GetSocketPath()
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	if follow {
		return streamLogs(client, name)
	}

	logs, err := client.GetLogs(name)
	if err != nil {
		return err
	}

	if len(logs) == 0 {
		fmt.Printf("No logs available for agent: %s\n", name)
		return nil
	}

	// Show last N lines if specified
	start := 0
	if lines > 0 && len(logs) > lines {
		start = len(logs) - lines
	}

	for i := start; i < len(logs); i++ {
		fmt.Println(logs[i])
	}

	return nil
}

func streamLogs(client *ipc.Client, name string) error {
	// Setup signal handling for Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logs, err := client.GetLogs(name)
	if err != nil {
		return err
	}

	// Print existing logs
	for _, log := range logs {
		fmt.Println(log)
	}

	fmt.Printf("--- Following logs for %s (Press Ctrl+C to exit) ---\n", name)

	// Start streaming new logs
	lastCount := len(logs)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sigChan:
			fmt.Printf("\nStopping log stream for %s\n", name)
			return nil

		case <-ticker.C:
			newLogs, err := client.GetLogs(name)
			if err != nil {
				fmt.Printf("Error fetching logs: %v\n", err)
				continue
			}

			// Print any new log lines
			if len(newLogs) > lastCount {
				for i := lastCount; i < len(newLogs); i++ {
					fmt.Println(newLogs[i])
				}
				lastCount = len(newLogs)
			}
		}
	}
}
