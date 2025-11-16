package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"opperator/internal/cli"
	"opperator/internal/credentials"
	"opperator/internal/daemon"
	"opperator/internal/deployment"
	"opperator/internal/onboarding"
	"opperator/updater"
	"opperator/version"
	"tui"
)

var (
	tuiCPUProfilePath string
)

var rootCmd = &cobra.Command{
	Use:   "op",
	Short: "Opperator",
	Run: func(cmd *cobra.Command, args []string) {
		// Check if this is the first run
		if onboarding.IsFirstRun() {
			fmt.Println("Welcome to Opperator! Let's get you set up.")
			if err := onboarding.RunWizard(); err != nil {
				log.Fatalf("Setup failed: %v", err)
			}
			return
		}

		// Ensure daemon is running
		if !daemon.IsRunning() {
			fmt.Printf("Daemon is not running. Starting services...\n")
			if err := onboarding.StartServices(); err != nil {
				fmt.Printf("Failed to start services: %v\n", err)
				fmt.Println("Try running: op daemon start")
				os.Exit(1)
			}
			fmt.Println("Services started successfully!")

			// Give services time to start
			time.Sleep(2 * time.Second)
		}

		var stopProfile func()
		if tuiCPUProfilePath != "" {
			cleanup, err := startTUICPUProfile(tuiCPUProfilePath)
			if err != nil {
				log.Fatalf("failed to start CPU profiling: %v", err)
			}
			stopProfile = cleanup
			defer func() {
				if stopProfile != nil {
					stopProfile()
				}
			}()
		}

		hasKey, err := credentials.HasAPIKey()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading secrets: %v\n", err)
			os.Exit(1)
		}
		if !hasKey {
			fmt.Fprintf(os.Stderr, "Opper API key is not configured. Run `op secret create %s` to add one.\n", credentials.OpperAPIKeyName)
			os.Exit(1)
		}

		// Open TUI interface
		if err := tui.Start(); err != nil {
			if stopProfile != nil {
				stopProfile()
				stopProfile = nil
			}
			log.Fatal(err)
		}
	},
}

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage local and remote daemon connections",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the local daemon (runs in background by default)",
	Run: func(cmd *cobra.Command, args []string) {
		foreground, _ := cmd.Flags().GetBool("foreground")

		// Check if daemon is already running
		if daemon.IsRunning() {
			fmt.Println("Daemon is already running")
			os.Exit(1)
		}

		// Clean up any stale files from previous crash
		if err := daemon.CleanupStaleFiles(); err != nil {
			if err.Error() == "daemon is actually running" {
				fmt.Println("Daemon is already running")
				os.Exit(1)
			}
			// Ignore other cleanup errors, just log them
			log.Printf("Warning: cleanup failed: %v", err)
		}

		// If not foreground mode, start in background and exit
		if !foreground {
			executable, err := os.Executable()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get executable path: %v\n", err)
				os.Exit(1)
			}

			// Start daemon in background with --foreground flag
			daemonCmd := exec.Command(executable, "daemon", "start", "--foreground")
			if err := daemonCmd.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to start daemon: %v\n", err)
				os.Exit(1)
			}

			fmt.Println("Starting daemon in background...")

			// Wait for daemon to be running
			for i := 0; i < 10; i++ {
				time.Sleep(500 * time.Millisecond)
				if daemon.IsRunning() {
					fmt.Println("Daemon started successfully")
					return
				}
			}

			fmt.Println("Warning: Daemon may not have started properly")
			return
		}

		// Foreground mode - run daemon directly
		// Make this daemon process a process group leader
		// This allows us to kill the daemon and all its children with one signal
		if err := syscall.Setpgid(0, 0); err != nil {
			log.Printf("Warning: failed to create process group: %v", err)
		} else {
			log.Printf("Daemon running in process group: %d", os.Getpid())
		}

		server, err := daemon.NewServer()
		if err != nil {
			if errors.Is(err, daemon.ErrAlreadyRunning) {
				fmt.Println("Daemon is already running")
				os.Exit(1)
			}
			log.Fatalf("Failed to create server: %v", err)
		}

		// Handle graceful shutdown
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			sig := <-sigChan
			log.Printf("Received signal %v, initiating graceful shutdown...", sig)
			fmt.Println("\nShutting down daemon...")
			server.Stop()
			log.Printf("Graceful shutdown complete")
			os.Exit(0)
		}()

		if err := server.Start(); err != nil {
			log.Fatal(err)
		}
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the local daemon",
	Run: func(cmd *cobra.Command, args []string) {
		if !daemon.IsRunning() {
			fmt.Println("Daemon is not running")
			os.Exit(1)
		}

		// Get PID from daemon
		pid, err := daemon.ReadPIDFile()
		if err != nil {
			fmt.Printf("Error reading PID: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Stopping daemon (PID: %d)...\n", pid)

		// Use comprehensive shutdown that kills all processes
		if err := daemon.Shutdown(pid, nil); err != nil {
			fmt.Printf("Warning: %v\n", err)
			fmt.Println("Daemon stop completed with warnings")
			os.Exit(1)
		}

		// Clean up PID file and socket
		if err := daemon.CleanupStaleFiles(); err != nil {
			// Ignore cleanup errors, daemon is already stopped
			log.Printf("Warning: cleanup failed: %v", err)
		}

		fmt.Println("Daemon stopped successfully")
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check local daemon status",
	Run: func(cmd *cobra.Command, args []string) {
		if daemon.IsRunning() {
			fmt.Println("Daemon is running")
			if err := cli.ShowToolTaskMetrics(); err != nil {
				fmt.Fprintf(os.Stderr, "(warning) failed to fetch async metrics: %v\n", err)
			}
		} else {
			fmt.Println("Daemon is not running")
		}
	},
}

var daemonAddCmd = &cobra.Command{
	Use:   "add [name] [address]",
	Short: "Add or update a daemon connection",
	Long: `Add a new daemon to the registry or update an existing one.

Address formats:
  unix:///path/to/socket.sock  (local Unix socket)
  tcp://hostname:port          (remote TCP connection)

Examples:
  op daemon add local unix:///tmp/opperator.sock
  op daemon add production tcp://vps.example.com:9999 --token=$PROD_TOKEN
  op daemon add staging tcp://192.168.1.100:9999 --enabled=false`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		address := args[1]
		token, _ := cmd.Flags().GetString("token")
		enabled, _ := cmd.Flags().GetBool("enabled")

		if err := cli.AddDaemon(name, address, token, enabled); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var daemonListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured daemons",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cli.ListDaemons(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var daemonRemoveCmd = &cobra.Command{
	Use:   "remove [name]",
	Short: "Remove a daemon from the registry",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		force, _ := cmd.Flags().GetBool("force")
		if err := cli.RemoveDaemon(args[0], force); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var daemonTestCmd = &cobra.Command{
	Use:   "test [name]",
	Short: "Test connectivity to a daemon",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := cli.TestDaemon(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var daemonEnableCmd = &cobra.Command{
	Use:   "enable [name]",
	Short: "Enable a daemon connection",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := cli.SetDaemonEnabled(args[0], true); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var daemonDisableCmd = &cobra.Command{
	Use:   "disable [name]",
	Short: "Disable a daemon connection",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := cli.SetDaemonEnabled(args[0], false); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var cloudCmd = &cobra.Command{
	Use:   "cloud",
	Short: "Manage cloud deployments",
	Long:  `Deploy and manage Opperator daemons on cloud providers like Hetzner.`,
}

var cloudDeployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy a daemon to a cloud VPS",
	Long: `Deploy an Opperator daemon to a new cloud VPS (currently supports Hetzner Cloud).

This will:
 1. Create a new VPS server on your cloud provider
 2. Install and configure Opperator
 3. Register the daemon in your local config

The wizard will guide you through the process.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := deployment.Deploy(); err != nil {
			if err.Error() == "cancelled" {
				fmt.Println("\nDeployment cancelled.")
				return
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var cloudDestroyCmd = &cobra.Command{
	Use:   "destroy [name]",
	Short: "Destroy a cloud deployment and delete its VPS",
	Long: `Destroy a cloud-deployed daemon and delete its VPS.

⚠️  WARNING: This will permanently delete the server and all data.
Billing will stop immediately.

Use --force to skip the confirmation prompt.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		force, _ := cmd.Flags().GetBool("force")
		if err := deployment.Destroy(args[0], force); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var cloudListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all cloud deployments",
	Long:  `List all Opperator daemons deployed to cloud providers.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := cli.ListCloudDaemons(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var cloudUpdateCmd = &cobra.Command{
	Use:   "update [daemon-name]",
	Short: "Update a cloud daemon with the latest binary",
	Long: `Update a cloud-deployed daemon by:
 1. Building a new Linux binary from your current code
 2. Transferring it to the remote server via SSH
 3. Gracefully restarting the daemon

This is useful after you've made local changes and want to deploy them to production.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		preRelease, _ := cmd.Flags().GetBool("pre-release")
		if err := deployment.Update(args[0], preRelease); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var asyncCmd = &cobra.Command{
	Use:   "async",
	Short: "Inspect daemon async tasks",
}

var asyncListCmd = &cobra.Command{
	Use:   "list",
	Short: "List async tasks managed by the daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		status, _ := cmd.Flags().GetString("status")
		origin, _ := cmd.Flags().GetString("origin")
		session, _ := cmd.Flags().GetString("session")
		client, _ := cmd.Flags().GetString("client")
		return cli.ListAsyncTasks(cli.AsyncListOptions{
			Status:  status,
			Origin:  origin,
			Session: session,
			Client:  client,
		})
	},
}

var asyncGetCmd = &cobra.Command{
	Use:   "get [task_id]",
	Short: "Show details for a specific async task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return cli.ShowAsyncTask(args[0])
	},
}

var asyncDeleteCmd = &cobra.Command{
	Use:   "delete [task_id]",
	Short: "Delete an async task by id",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return cli.DeleteAsyncTask(args[0])
	},
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Run the setup wizard",
	Run: func(cmd *cobra.Command, args []string) {
		if err := onboarding.RunWizard(); err != nil {
			log.Fatalf("Setup failed: %v", err)
		}
	},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check installation and runtime health",
	Run: func(cmd *cobra.Command, args []string) {
		exitCode, err := cli.Doctor(cmd.OutOrStdout())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	},
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all agents from all daemons",
	Run: func(cmd *cobra.Command, args []string) {
		runningOnly, _ := cmd.Flags().GetBool("running")
		stoppedOnly, _ := cmd.Flags().GetBool("stopped")
		crashedOnly, _ := cmd.Flags().GetBool("crashed")
		daemonFilter, _ := cmd.Flags().GetString("daemon")

		// Ensure only one filter is used at a time
		filters := 0
		if runningOnly {
			filters++
		}
		if stoppedOnly {
			filters++
		}
		if crashedOnly {
			filters++
		}
		if filters > 1 {
			fmt.Fprintln(os.Stderr, "Use at most one of --running, --stopped, --crashed")
			os.Exit(1)
		}
		if err := cli.ListAgents(runningOnly, stoppedOnly, crashedOnly, daemonFilter); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var startCmd = &cobra.Command{
	Use:   "start [name]",
	Short: "Start an agent (auto-detects daemon or use --daemon)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		daemon, _ := cmd.Flags().GetString("daemon")
		if err := cli.StartAgent(args[0], daemon); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop [name]",
	Short: "Stop an agent (auto-detects daemon or use --daemon)",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		stopAll, _ := cmd.Flags().GetBool("all")
		daemon, _ := cmd.Flags().GetString("daemon")

		if stopAll {
			if err := cli.StopAllAgents(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		} else if len(args) == 1 {
			if err := cli.StopAgent(args[0], daemon); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Fprintln(os.Stderr, "Error: specify agent name or use -a flag")
			os.Exit(1)
		}
	},
}

var restartCmd = &cobra.Command{
	Use:   "restart [name]",
	Short: "Restart an agent (auto-detects daemon or use --daemon)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		daemon, _ := cmd.Flags().GetString("daemon")
		if err := cli.RestartAgent(args[0], daemon); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap [name]",
	Short: "Bootstrap a new agent with SDK and templates",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		description, _ := cmd.Flags().GetString("description")
		noStart, _ := cmd.Flags().GetBool("no-start")
		if err := cli.BootstrapAgent(args[0], description, noStart); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete [name]",
	Short: "Delete an agent and all its data",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		force, _ := cmd.Flags().GetBool("force")
		daemonName, _ := cmd.Flags().GetString("daemon")
		if err := cli.DeleteAgent(args[0], force, daemonName); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var moveCmd = &cobra.Command{
	Use:   "move [agent-name] --to [daemon-name]",
	Short: "Move an agent to another daemon",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		toDaemon, _ := cmd.Flags().GetString("to")
		force, _ := cmd.Flags().GetBool("force")
		noStart, _ := cmd.Flags().GetBool("no-start")

		if toDaemon == "" {
			fmt.Fprintf(os.Stderr, "Error: --to flag is required\n")
			os.Exit(1)
		}

		if err := cli.MoveAgent(args[0], toDaemon, force, noStart); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var whereCmd = &cobra.Command{
	Use:   "where [agent-name]",
	Short: "Find which daemon has an agent",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := cli.WhereIsAgent(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var reloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Reload configuration (use --daemon to specify which daemon)",
	Run: func(cmd *cobra.Command, args []string) {
		daemon, _ := cmd.Flags().GetString("daemon")
		if err := cli.ReloadConfig(daemon); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var logsCmd = &cobra.Command{
	Use:   "logs [name]",
	Short: "Get logs from an agent (auto-detects daemon or use --daemon)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		follow, _ := cmd.Flags().GetBool("follow")
		lines, _ := cmd.Flags().GetInt("lines")
		daemon, _ := cmd.Flags().GetString("daemon")

		if err := cli.GetLogs(args[0], follow, lines, daemon); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var commandCmd = &cobra.Command{
	Use:   "command [name] [command] [args...]",
	Short: "Send a command to a managed agent (auto-detects daemon or use --daemon)",
	Long: `Send a command to a managed agent with either natural language arguments or JSON.

Examples:
  # Natural language arguments (LLM-parsed)
  op agent command weather-agent get_forecast "from 2nd march to 10th march in London"

  # JSON arguments (precise control)
  op agent command weather-agent get_forecast --args '{"start":"2024-03-02","end":"2024-03-10","city":"London"}'

  # No arguments
  op agent command my-agent refresh`,
	Args: cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		agentName := args[0]
		commandName := args[1]

		argsJSON, _ := cmd.Flags().GetString("args")
		timeout, _ := cmd.Flags().GetDuration("timeout")
		daemon, _ := cmd.Flags().GetString("daemon")

		// Check if raw text args provided (everything after command name)
		if len(args) > 2 && argsJSON == "" {
			// Join all remaining args as raw input
			rawInput := strings.Join(args[2:], " ")

			// Parse using LLM
			if err := cli.InvokeCommandWithParsing(agentName, commandName, rawInput, timeout, daemon); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Fallback to --args JSON mode or no args
		var payload map[string]interface{}
		if argsJSON != "" {
			if err := json.Unmarshal([]byte(argsJSON), &payload); err != nil {
				fmt.Fprintf(os.Stderr, "Invalid JSON payload: %v\n", err)
				os.Exit(1)
			}
		}

		if err := cli.InvokeCommand(agentName, commandName, payload, timeout, daemon); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var listCommandsCmd = &cobra.Command{
	Use:   "commands [name]",
	Short: "List available commands for an agent (auto-detects daemon or use --daemon)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		daemon, _ := cmd.Flags().GetString("daemon")
		if err := cli.ListAgentCommands(args[0], daemon); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage secrets in the system keyring (OPPER_API_KEY reserved for Opper)",
}

var secretCreateCmd = &cobra.Command{
	Use:   "create [name] [value]",
	Short: "Store a new secret value",
	Args:  cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		value := ""
		if len(args) > 1 {
			value = args[1]
		}
		if err := cli.CreateSecret(name, value); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var secretUpdateCmd = &cobra.Command{
	Use:   "update [name] [value]",
	Short: "Replace a stored secret value",
	Args:  cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		value := ""
		if len(args) > 1 {
			value = args[1]
		}
		if err := cli.UpdateSecret(name, value); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var secretDeleteCmd = &cobra.Command{
	Use:   "delete [name]",
	Short: "Remove a stored secret",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if err := cli.DeleteSecret(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var secretReadCmd = &cobra.Command{
	Use:   "read [name]",
	Short: "Read a secret value",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		value, err := cli.ReadSecret(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(value)
	},
}

var secretListCmd = &cobra.Command{
	Use:   "list",
	Short: "List secrets registered with opperator",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cli.ListSecrets(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var secretStatusCmd = &cobra.Command{
	Use:   "status [name]",
	Short: "Check whether a secret exists",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if err := cli.SecretStatus(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Version information and update commands",
}

var versionShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("opperator version %s\n", version.Get())
	},
}

var versionCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check for available updates",
	Run: func(cmd *cobra.Command, args []string) {
		includePrerelease, _ := cmd.Flags().GetBool("pre-release")
		fmt.Println("Checking for updates...")
		info, err := updater.CheckForUpdates(includePrerelease)
		if err != nil {
			// Handle "no releases" gracefully
			if strings.Contains(err.Error(), "no releases found") {
				fmt.Printf("Current version: %s\n", version.Get())
				fmt.Println("\n✓ No releases published yet on GitHub")
				fmt.Println("Once you publish a release, the updater will be able to check for updates")
				return
			}
			fmt.Fprintf(os.Stderr, "Error checking for updates: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Current version: %s\n", info.CurrentVersion)
		fmt.Printf("Latest version:  %s\n", info.LatestVersion)

		if info.Available {
			fmt.Printf("\n✓ Update available!\n")
			fmt.Printf("Run 'op version update' to install version %s\n", info.LatestVersion)
		} else {
			fmt.Println("\n✓ You are running the latest version")
		}
	},
}

var versionUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update to the latest version",
	Run: func(cmd *cobra.Command, args []string) {
		includePrerelease, _ := cmd.Flags().GetBool("pre-release")
		fmt.Println("Checking for updates...")
		info, err := updater.CheckForUpdates(includePrerelease)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking for updates: %v\n", err)
			os.Exit(1)
		}

		if !info.Available {
			fmt.Println("✓ You are already running the latest version")
			return
		}

		fmt.Printf("Current version: %s\n", info.CurrentVersion)
		fmt.Printf("Latest version:  %s\n\n", info.LatestVersion)
		fmt.Println("Downloading and installing update...")

		if err := updater.DownloadAndInstall(info); err != nil {
			fmt.Fprintf(os.Stderr, "Error installing update: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("\n✓ Successfully updated to version %s\n", info.LatestVersion)

		// Stop local daemon if running
		if daemon.IsRunning() {
			fmt.Println("\nStopping local daemon...")
			pid, err := daemon.ReadPIDFile()
			if err != nil {
				fmt.Printf("Warning: Failed to read daemon PID: %v\n", err)
			} else {
				if err := daemon.Shutdown(pid, nil); err != nil {
					fmt.Printf("Warning: Failed to stop local daemon: %v\n", err)
				} else {
					// Clean up PID file and socket
					if err := daemon.CleanupStaleFiles(); err != nil {
						log.Printf("Warning: cleanup failed: %v", err)
					}
					fmt.Println("✓ Local daemon stopped")
				}
			}
		}

		// Update all cloud daemons
		if err := deployment.UpdateAllCloudDaemons(includePrerelease); err != nil {
			fmt.Printf("Warning: Some cloud daemon updates may have failed: %v\n", err)
		}

		fmt.Println("\nUpdate complete! The daemon will start automatically when you run 'op' again.")
	},
}

var execCmd = &cobra.Command{
	Use:   "exec [message]",
	Short: "Send a message to an agent and get the response",
	Long: `Send a message to an agent and receive the response.

Activity is streamed to stderr, while the final assistant response is written to stdout.
This allows piping the output to other commands while still seeing progress.

Examples:
  op exec "What is the weather today?" --agent weather-bot
  op exec "Continue our discussion" --resume 1234567890
  op exec "Hello" --agent assistant | jq -r .`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		message := args[0]
		agentName, _ := cmd.Flags().GetString("agent")
		conversationID, _ := cmd.Flags().GetString("resume")
		jsonMode, _ := cmd.Flags().GetBool("json")
		noSave, _ := cmd.Flags().GetBool("no-save")

		if err := cli.ExecMessage(message, agentName, conversationID, jsonMode, noSave); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func startTUICPUProfile(path string) (func(), error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create cpu profile file: %w", err)
	}

	if err := pprof.StartCPUProfile(file); err != nil {
		file.Close()
		return nil, fmt.Errorf("start cpu profile: %w", err)
	}

	fmt.Printf("Recording TUI CPU profile to %s\n", path)

	return func() {
		pprof.StopCPUProfile()
		file.Close()
		fmt.Printf("Saved TUI CPU profile to %s\n", path)
	}, nil
}

func init() {
	// Disable the default completion command
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.Flags().StringVar(&tuiCPUProfilePath, "tui-cpuprofile", "", "Write TUI CPU profile to file")
	stopCmd.Flags().BoolP("all", "a", false, "Stop all agents")
	stopCmd.Flags().String("daemon", "", "Specify daemon (auto-detects if not provided)")
	logsCmd.Flags().BoolP("follow", "f", false, "Follow log output (stream mode)")
	logsCmd.Flags().IntP("lines", "n", 0, "Show last N lines (0 = all lines)")
	logsCmd.Flags().String("daemon", "", "Specify daemon (auto-detects if not provided)")
	startCmd.Flags().String("daemon", "", "Specify daemon (auto-detects if not provided)")
	restartCmd.Flags().String("daemon", "", "Specify daemon (auto-detects if not provided)")
	reloadCmd.Flags().String("daemon", "", "Specify daemon to reload (defaults to local)")
	commandCmd.Flags().String("args", "", "JSON object to pass as command arguments")
	commandCmd.Flags().Duration("timeout", 10*time.Second, "How long to wait for the command response")
	commandCmd.Flags().String("daemon", "", "Specify daemon (auto-detects if not provided)")
	listCommandsCmd.Flags().String("daemon", "", "Specify daemon (auto-detects if not provided)")

	listCmd.Flags().Bool("running", false, "Only show running agents")
	listCmd.Flags().Bool("stopped", false, "Only show stopped agents")
	listCmd.Flags().Bool("crashed", false, "Only show crashed agents")
	listCmd.Flags().String("daemon", "", "Filter agents by daemon name")
	bootstrapCmd.Flags().StringP("description", "d", "", "Agent description")
	bootstrapCmd.Flags().Bool("no-start", false, "Skip auto-starting the agent after bootstrap")
	deleteCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompt")
	deleteCmd.Flags().String("daemon", "", "Daemon to delete from (auto-detected if not specified)")
	moveCmd.Flags().String("to", "", "Target daemon name (required)")
	moveCmd.Flags().BoolP("force", "f", false, "Overwrite if agent exists on destination")
	moveCmd.Flags().Bool("no-start", false, "Don't auto-start agent on destination")
	agentCmd.AddCommand(listCmd)
	agentCmd.AddCommand(startCmd)
	agentCmd.AddCommand(stopCmd)
	agentCmd.AddCommand(restartCmd)
	agentCmd.AddCommand(bootstrapCmd)
	agentCmd.AddCommand(deleteCmd)
	agentCmd.AddCommand(moveCmd)
	agentCmd.AddCommand(whereCmd)
	agentCmd.AddCommand(reloadCmd)
	agentCmd.AddCommand(logsCmd)
	agentCmd.AddCommand(commandCmd)
	agentCmd.AddCommand(listCommandsCmd)
	secretCmd.AddCommand(secretCreateCmd)
	secretCmd.AddCommand(secretUpdateCmd)
	secretCmd.AddCommand(secretDeleteCmd)
	secretCmd.AddCommand(secretReadCmd)
	secretCmd.AddCommand(secretListCmd)
	secretCmd.AddCommand(secretStatusCmd)

	// Daemon start flags
	daemonStartCmd.Flags().Bool("foreground", false, "Run daemon in foreground (blocks terminal)")

	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonAddCmd)
	daemonCmd.AddCommand(daemonListCmd)
	daemonCmd.AddCommand(daemonRemoveCmd)
	daemonCmd.AddCommand(daemonTestCmd)
	daemonCmd.AddCommand(daemonEnableCmd)
	daemonCmd.AddCommand(daemonDisableCmd)

	// Cloud command
	cloudCmd.AddCommand(cloudDeployCmd)
	cloudCmd.AddCommand(cloudDestroyCmd)
	cloudCmd.AddCommand(cloudListCmd)
	cloudCmd.AddCommand(cloudUpdateCmd)

	// Cloud update flags
	cloudUpdateCmd.Flags().Bool("pre-release", false, "Update to the latest pre-release version instead of stable")

	// Daemon add flags
	daemonAddCmd.Flags().String("token", "", "Authentication token (can use env var: --token=$MY_TOKEN)")
	daemonAddCmd.Flags().Bool("enabled", true, "Enable the daemon connection")

	// Daemon remove flags
	daemonRemoveCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompt")

	// Cloud destroy flags
	cloudDestroyCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompt")

	asyncListCmd.Flags().String("status", "", "Filter tasks by status (pending|complete|failed)")
	asyncListCmd.Flags().String("origin", "", "Filter tasks by origin identifier")
	asyncListCmd.Flags().String("session", "", "Filter tasks by session identifier")
	asyncListCmd.Flags().String("client", "", "Filter tasks by client identifier")
	asyncCmd.AddCommand(asyncListCmd)
	asyncCmd.AddCommand(asyncGetCmd)
	asyncCmd.AddCommand(asyncDeleteCmd)

	// Add version subcommands
	versionCheckCmd.Flags().Bool("pre-release", false, "Include pre-release versions")
	versionUpdateCmd.Flags().Bool("pre-release", false, "Include pre-release versions")
	versionCmd.AddCommand(versionShowCmd)
	versionCmd.AddCommand(versionCheckCmd)
	versionCmd.AddCommand(versionUpdateCmd)

	// Add exec command flags
	execCmd.Flags().String("agent", "", "Name of the agent to send the message to")
	execCmd.Flags().String("resume", "", "Resume an existing conversation by ID")
	execCmd.Flags().Bool("json", false, "Output events as JSON Lines (JSONL) instead of pretty-printing")
	execCmd.Flags().Bool("no-save", false, "Don't save conversation to database")

	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(secretCmd)
	rootCmd.AddCommand(asyncCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(cloudCmd)
	rootCmd.AddCommand(execCmd)
	// Add hidden commands (needed internally but not shown to users)
	rootCmd.AddCommand(daemonCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
