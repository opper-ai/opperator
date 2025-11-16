package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"opperator/config"
	"opperator/internal/credentials"
	"opperator/internal/ipc"
	"opperator/pkg/argparser"
	"tui/opper"
)

// Styles for CLI output (matching exec.go styles)
var (
	cmdPrimary   = lipgloss.Color("#f7c0af") // orangish/peach
	cmdSecondary = lipgloss.Color("#3ccad7") // cyan
	cmdSuccess   = lipgloss.Color("#87bf47") // green
	cmdError     = lipgloss.Color("#bf5d47") // red
	cmdMuted     = lipgloss.Color("#7f7f7f") // gray
)

// getCommandStyles returns styles with proper color profile detection for stderr
func getCommandStyles() (label, value, muted, success, errorStyle, bracket lipgloss.Style) {
	// Detect color profile from stderr (not stdout, since that might be redirected)
	renderer := lipgloss.NewRenderer(os.Stderr)

	label = renderer.NewStyle().Foreground(cmdPrimary).Bold(true)
	value = renderer.NewStyle().Foreground(cmdSecondary)
	muted = renderer.NewStyle().Foreground(cmdMuted)
	success = renderer.NewStyle().Foreground(cmdSuccess)
	errorStyle = renderer.NewStyle().Foreground(cmdError)
	bracket = renderer.NewStyle().Foreground(cmdMuted)

	return
}

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
			return fmt.Errorf("daemon is not running. Start it with: op daemon start")
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

func DeleteAgent(name string, force bool, daemonName string) error {
	// Find which daemon has the agent
	if daemonName == "" {
		foundDaemon, err := findAgentDaemon(name)
		if err != nil {
			return err
		}
		daemonName = foundDaemon
	}

	// Get client for the daemon
	client, err := ipc.NewClientFromRegistry(daemonName)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon '%s': %w", daemonName, err)
	}
	defer client.Close()

	// Show what will be deleted
	fmt.Printf("This will permanently delete agent '%s' from daemon '%s' and all its data:\n", name, daemonName)
	fmt.Println()
	fmt.Println("  - Agent directory and all files")
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

	fmt.Printf("Deleting agent '%s' from daemon '%s'...\n", name, daemonName)
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
			return fmt.Errorf("daemon is not running. Start it with: op daemon start")
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

func ReloadConfig(daemonName string) error {
	// Default to local daemon if not specified
	if daemonName == "" {
		daemonName = "local"
	}

	client, err := ipc.NewClientFromRegistry(daemonName)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon '%s' is not running", daemonName)
		}
		return err
	}
	defer client.Close()

	if err := client.ReloadConfig(); err != nil {
		return err
	}
	fmt.Printf("Configuration reloaded successfully on daemon '%s'\n", daemonName)
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

	// Get styles with proper stderr detection
	_, valueStyle, mutedStyle, successStyle, _, _ := getCommandStyles()

	// Activity/status to stderr (styled)
	fmt.Fprintln(os.Stderr, successStyle.Render("✓")+" Command "+valueStyle.Render("'"+command+"'")+" succeeded on agent "+valueStyle.Render("'"+name+"'")+" "+mutedStyle.Render("(daemon: "+foundDaemon+")"))

	// Result to stdout
	if resp.Result != nil {
		if data, err := json.MarshalIndent(resp.Result, "", "  "); err == nil {
			fmt.Println(string(data))
		} else {
			fmt.Printf("%v\n", resp.Result)
		}
	}
	return nil
}

// opperClientAdapter adapts tui/opper.Opper to argparser.OpperClient
type opperClientAdapter struct {
	client *opper.Opper
}

func (a *opperClientAdapter) Stream(ctx context.Context, req argparser.StreamRequest) (<-chan argparser.SSEEvent, error) {
	// Convert argparser.StreamRequest to opper.StreamRequest
	opperReq := opper.StreamRequest{
		Name:         req.Name,
		Instructions: req.Instructions,
		Input:        req.Input,
		OutputSchema: req.OutputSchema,
	}

	// Call the underlying opper client
	opperEvents, err := a.client.Stream(ctx, opperReq)
	if err != nil {
		return nil, err
	}

	// Create a channel to adapt the events
	adaptedEvents := make(chan argparser.SSEEvent)

	// Start a goroutine to convert events
	go func() {
		defer close(adaptedEvents)
		for event := range opperEvents {
			adaptedEvents <- argparser.SSEEvent{
				Data: argparser.ChunkData{
					JSONPath:  event.Data.JSONPath,
					ChunkType: event.Data.ChunkType,
					Delta:     event.Data.Delta,
				},
			}
		}
	}()

	return adaptedEvents, nil
}

// jsonAggregatorAdapter adapts tui/opper.JSONChunkAggregator to argparser.JSONAggregator
type jsonAggregatorAdapter struct {
	aggregator *opper.JSONChunkAggregator
}

func (a *jsonAggregatorAdapter) Add(path string, delta interface{}) {
	a.aggregator.Add(path, delta)
}

func (a *jsonAggregatorAdapter) Assemble() (string, error) {
	return a.aggregator.Assemble()
}

func InvokeCommandWithParsing(name, command, rawInput string, timeout time.Duration, daemonName string) error {
	client, foundDaemon, err := getClientForAgent(name, daemonName)
	if err != nil {
		return err
	}
	defer client.Close()

	// Get styles with proper stderr detection
	labelStyle, valueStyle, mutedStyle, _, _, _ := getCommandStyles()

	// Fetch command descriptors to get the argument schema (stderr)
	fmt.Fprintln(os.Stderr, mutedStyle.Render("Fetching command schema for")+valueStyle.Render(" '"+command+"' ")+" on agent "+valueStyle.Render("'"+name+"'")+"...")
	commands, err := client.ListCommands(name)
	if err != nil {
		return fmt.Errorf("failed to get command schema: %w", err)
	}

	// Find the command schema
	var schema []argparser.CommandArgument
	for _, cmd := range commands {
		if cmd.Name == command {
			// Convert protocol.CommandArgument to argparser.CommandArgument
			schema = make([]argparser.CommandArgument, len(cmd.Arguments))
			for i, arg := range cmd.Arguments {
				schema[i] = argparser.CommandArgument{
					Name:        arg.Name,
					Type:        arg.Type,
					Description: arg.Description,
					Required:    arg.Required,
					Default:     arg.Default,
					Enum:        arg.Enum,
					Items:       arg.Items,
					Properties:  arg.Properties,
				}
			}
			break
		}
	}

	if schema == nil {
		return fmt.Errorf("command '%s' not found on agent '%s'", command, name)
	}

	// If no arguments are expected, just invoke the command directly
	if len(schema) == 0 {
		fmt.Fprintln(os.Stderr, mutedStyle.Render("Command")+valueStyle.Render(" '"+command+"' ")+" expects no arguments, invoking directly...")
		return InvokeCommand(name, command, nil, timeout, daemonName)
	}

	// Get API key
	apiKey, err := credentials.GetSecret(credentials.OpperAPIKeyName)
	if err != nil {
		return fmt.Errorf("failed to read Opper API key: %w (run: op secret create %s)", err, credentials.OpperAPIKeyName)
	}

	// Parse the raw input using LLM (stderr)
	fmt.Fprintln(os.Stderr, labelStyle.Render("Parsing arguments:")+" "+valueStyle.Render("\""+rawInput+"\""))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create Opper client and aggregator with adapters
	opperClient := &opperClientAdapter{client: opper.New(apiKey)}
	aggregator := &jsonAggregatorAdapter{aggregator: opper.NewJSONChunkAggregator()}

	args, err := argparser.ParseCommandArguments(ctx, apiKey, rawInput, schema, opperClient, aggregator)
	if err != nil {
		return fmt.Errorf("failed to parse arguments: %w", err)
	}

	// Display parsed arguments (stderr)
	fmt.Fprintln(os.Stderr, labelStyle.Render("Parsed arguments:"))
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "  "+mutedStyle.Render("(no arguments)"))
	} else {
		for k, v := range args {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", valueStyle.Render(k), valueStyle.Render(fmt.Sprintf("%v", v)))
		}
	}
	fmt.Fprintln(os.Stderr)

	// Now invoke the command with parsed args
	return InvokeCommand(name, command, args, timeout, foundDaemon)
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
			return fmt.Errorf("daemon is not running. Start it with: op daemon start")
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
