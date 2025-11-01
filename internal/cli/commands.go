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

// findAgentDaemon searches all daemons to find which one has the specified agent
// Returns the daemon name, or error if not found or ambiguous
func findAgentDaemon(agentName string) (string, error) {
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return "", fmt.Errorf("failed to load daemon registry: %w", err)
	}

	var foundDaemons []string

	for _, daemon := range registry.Daemons {
		if !daemon.Enabled {
			continue
		}

		client, err := ipc.NewClientWithAuth(daemon.Address, daemon.AuthToken)
		if err != nil {
			continue
		}

		processes, err := client.ListAgents()
		client.Close()
		if err != nil {
			continue
		}

		for _, p := range processes {
			if p.Name == agentName {
				foundDaemons = append(foundDaemons, daemon.Name)
				break
			}
		}
	}

	if len(foundDaemons) == 0 {
		return "", fmt.Errorf("agent '%s' not found on any daemon", agentName)
	}

	if len(foundDaemons) > 1 {
		return "", fmt.Errorf("agent '%s' exists on multiple daemons: %v. Please specify --daemon", agentName, foundDaemons)
	}

	return foundDaemons[0], nil
}

// getClientForAgent returns a client for the daemon that has the specified agent
// If daemonName is provided, uses that. Otherwise, searches for the agent.
func getClientForAgent(agentName, daemonName string) (*ipc.Client, string, error) {
	// If daemon not specified, find it
	if daemonName == "" {
		foundDaemon, err := findAgentDaemon(agentName)
		if err != nil {
			return nil, "", err
		}
		daemonName = foundDaemon
	}

	// Get client for the daemon
	client, err := ipc.NewClientFromRegistry(daemonName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to daemon '%s': %w", daemonName, err)
	}

	return client, daemonName, nil
}

func ListAgents(runningOnly, stoppedOnly, crashedOnly bool, daemonFilter string) error {
	// Load daemon registry
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return fmt.Errorf("failed to load daemon registry: %w", err)
	}

	// Collect agents from all daemons
	type AgentWithDaemon struct {
		Agent      *ipc.ProcessInfo
		DaemonName string
	}
	var allAgents []AgentWithDaemon

	for _, daemon := range registry.Daemons {
		// Skip if filtering by daemon
		if daemonFilter != "" && daemon.Name != daemonFilter {
			continue
		}

		// Skip disabled daemons
		if !daemon.Enabled {
			continue
		}

		// Connect to daemon
		client, err := ipc.NewClientWithAuth(daemon.Address, daemon.AuthToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to connect to daemon '%s': %v\n", daemon.Name, err)
			continue
		}

		// List agents
		processes, err := client.ListAgents()
		client.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to list agents from '%s': %v\n", daemon.Name, err)
			continue
		}

		// Add to collection
		for _, p := range processes {
			allAgents = append(allAgents, AgentWithDaemon{
				Agent:      p,
				DaemonName: daemon.Name,
			})
		}
	}

	if len(allAgents) == 0 {
		fmt.Println("No agents configured")
		return nil
	}

	fmt.Printf("%-15s %-20s %-10s %-10s %-8s %s\n", "DAEMON", "NAME", "STATUS", "PID", "UPTIME", "DESCRIPTION")
	fmt.Printf("%-15s %-20s %-10s %-10s %-8s %s\n", "------", "----", "------", "---", "------", "-----------")

	for _, item := range allAgents {
		p := item.Agent

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

		fmt.Printf("%-15s %-20s %-10s %-10s %-8s %s\n", item.DaemonName, p.Name, status, pid, uptime, desc)
	}

	return nil
}

func StartAgent(name, daemonName string) error {
	client, foundDaemon, err := getClientForAgent(name, daemonName)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.StartAgent(name); err != nil {
		return err
	}
	fmt.Printf("Started agent '%s' on daemon '%s'\n", name, foundDaemon)
	return nil
}

func StopAgent(name, daemonName string) error {
	client, foundDaemon, err := getClientForAgent(name, daemonName)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.StopAgent(name); err != nil {
		return err
	}
	fmt.Printf("Stopped agent '%s' on daemon '%s'\n", name, foundDaemon)
	return nil
}

func RestartAgent(name, daemonName string) error {
	client, foundDaemon, err := getClientForAgent(name, daemonName)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.RestartAgent(name); err != nil {
		return err
	}
	fmt.Printf("Restarted agent '%s' on daemon '%s'\n", name, foundDaemon)
	return nil
}

func BootstrapAgent(name, description string, noStart bool) error {
	client, err := ipc.NewClientFromRegistry("local")
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	fmt.Printf("Bootstrapping agent '%s'...\n", name)
	if description != "" {
		fmt.Printf("Description: %s\n", description)
	}
	fmt.Println()

	result, err := client.BootstrapAgent(name, description, noStart)
	if err != nil {
		return err
	}

	fmt.Println(result)

	// Get config directory for display
	configDir, _ := config.GetConfigDir()
	agentDir := fmt.Sprintf("%s/agents/%s", configDir, name)
	configFile, _ := config.GetConfigFile()

	fmt.Println()
	fmt.Println("Agent directory:")
	fmt.Printf("  cd %s\n", agentDir)
	fmt.Println()
	fmt.Println("Configuration updated:")
	fmt.Printf("  %s\n", configFile)
	fmt.Println()

	if noStart {
		fmt.Printf("Agent created but not started. Use 'op agent start %s' to start it.\n", name)
	} else {
		fmt.Printf("Agent '%s' is now running. Use 'opperator' to interact with it.\n", name)
	}

	return nil
}

func DeleteAgent(name string, force bool) error {
	client, err := ipc.NewClientFromRegistry("local")
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	// Get config directory to display what will be deleted
	configDir, _ := config.GetConfigDir()
	agentDir := fmt.Sprintf("%s/agents/%s", configDir, name)

	// Show what will be deleted
	fmt.Printf("This will permanently delete agent '%s' and all its data:\n", name)
	fmt.Println()
	fmt.Println("  - Agent directory and all files")
	fmt.Printf("    %s\n", agentDir)
	fmt.Println("  - Agent configuration entry (agents.yaml)")
	fmt.Println("  - Agent persistent data (agent_data.json)")
	fmt.Println("  - Agent logs (database and disk)")
	fmt.Println("  - All async tasks and history")
	fmt.Println()

	// Confirm unless --force is used
	if !force {
		fmt.Printf("Are you sure you want to delete agent '%s'? (y/N): ", name)
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Deletion cancelled.")
			return nil
		}
	}

	fmt.Printf("Deleting agent '%s'...\n", name)
	if err := client.DeleteAgent(name); err != nil {
		return err
	}

	fmt.Printf("Agent '%s' has been successfully deleted.\n", name)
	return nil
}

func StopAllAgents() error {
	client, err := ipc.NewClientFromRegistry("local")
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
	client, err := ipc.NewClientFromRegistry("local")
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

func InvokeCommand(name, command string, args map[string]interface{}, timeout time.Duration, daemonName string) error {
	client, foundDaemon, err := getClientForAgent(name, daemonName)
	if err != nil {
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

	fmt.Printf("Command '%s' succeeded on agent '%s' (daemon: %s)\n", command, name, foundDaemon)
	if resp.Result != nil {
		if data, err := json.MarshalIndent(resp.Result, "", "  "); err == nil {
			fmt.Println(string(data))
		} else {
			fmt.Printf("Result: %v\n", resp.Result)
		}
	}
	return nil
}

func ListAgentCommands(name, daemonName string) error {
	client, foundDaemon, err := getClientForAgent(name, daemonName)
	if err != nil {
		return err
	}
	defer client.Close()

	commands, err := client.ListCommands(name)
	if err != nil {
		return err
	}

	if len(commands) == 0 {
		fmt.Printf("Agent '%s' on daemon '%s' has no registered commands\n", name, foundDaemon)
		return nil
	}

	fmt.Printf("Commands for '%s' (daemon: %s):\n", name, foundDaemon)
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
	client, err := ipc.NewClientFromRegistry("local")
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

func GetLogs(name string, follow bool, lines int, daemonName string) error {
	client, foundDaemon, err := getClientForAgent(name, daemonName)
	if err != nil {
		return err
	}
	defer client.Close()

	if follow {
		fmt.Printf("Following logs for '%s' on daemon '%s'...\n", name, foundDaemon)
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
