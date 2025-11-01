package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"opperator/internal/cli"
	"opperator/internal/credentials"
	"opperator/internal/daemon"
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
				fmt.Println("Try running: ./opperator daemon")
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
			fmt.Fprintf(os.Stderr, "Opper API key is not configured. Run `./opperator secret create %s` to add one.\n", credentials.OpperAPIKeyName)
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
	Use:    "daemon",
	Short:  "Daemon management commands",
	Hidden: true, // Hide from help
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the daemon server",
	Run: func(cmd *cobra.Command, args []string) {
		// Make this daemon process a process group leader
		// This allows us to kill the daemon and all its children with one signal
		if err := syscall.Setpgid(0, 0); err != nil {
			log.Printf("Warning: failed to create process group: %v", err)
		} else {
			log.Printf("Daemon running in process group: %d", os.Getpid())
		}

		// First check if daemon is truly running
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

		// Double-check after cleanup - another process might have started during cleanup
		if daemon.IsRunning() {
			fmt.Println("Daemon is already running")
			os.Exit(1)
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
	Short: "Stop the daemon server",
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
	Short: "Check daemon status",
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

var daemonMetricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Show async task queue metrics",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !daemon.IsRunning() {
			return fmt.Errorf("daemon is not running")
		}
		if err := cli.ShowToolTaskMetrics(); err != nil {
			return err
		}
		return nil
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
	Use:   "get",
	Short: "Show details for a specific async task",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		return cli.ShowAsyncTask(id)
	},
}

var asyncDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete an async task by id",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		return cli.DeleteAsyncTask(id)
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
		if err := cli.DeleteAgent(args[0], force); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var reloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Reload configuration",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cli.ReloadConfig(); err != nil {
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
	Use:   "command [name] [command]",
	Short: "Send a command to a managed agent (auto-detects daemon or use --daemon)",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		argsJSON, _ := cmd.Flags().GetString("args")
		timeout, _ := cmd.Flags().GetDuration("timeout")
		daemon, _ := cmd.Flags().GetString("daemon")

		var payload map[string]interface{}
		if argsJSON != "" {
			if err := json.Unmarshal([]byte(argsJSON), &payload); err != nil {
				fmt.Fprintf(os.Stderr, "Invalid JSON payload: %v\n", err)
				os.Exit(1)
			}
		}

		if err := cli.InvokeCommand(args[0], args[1], payload, timeout, daemon); err != nil {
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
		fmt.Println("Please restart Opperator for the changes to take effect")
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
	agentCmd.AddCommand(listCmd)
	agentCmd.AddCommand(startCmd)
	agentCmd.AddCommand(stopCmd)
	agentCmd.AddCommand(restartCmd)
	agentCmd.AddCommand(bootstrapCmd)
	agentCmd.AddCommand(deleteCmd)
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

	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonMetricsCmd)
	daemonCmd.AddCommand(daemonAddCmd)
	daemonCmd.AddCommand(daemonListCmd)
	daemonCmd.AddCommand(daemonRemoveCmd)
	daemonCmd.AddCommand(daemonTestCmd)

	// Daemon add flags
	daemonAddCmd.Flags().String("token", "", "Authentication token (can use env var: --token=$MY_TOKEN)")
	daemonAddCmd.Flags().Bool("enabled", true, "Enable the daemon connection")

	// Daemon remove flags
	daemonRemoveCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompt")

	asyncListCmd.Flags().String("status", "", "Filter tasks by status (pending|complete|failed)")
	asyncListCmd.Flags().String("origin", "", "Filter tasks by origin identifier")
	asyncListCmd.Flags().String("session", "", "Filter tasks by session identifier")
	asyncListCmd.Flags().String("client", "", "Filter tasks by client identifier")
	asyncCmd.AddCommand(asyncListCmd)
	asyncGetCmd.Flags().String("id", "", "Task identifier (required)")
	asyncGetCmd.MarkFlagRequired("id")
	asyncDeleteCmd.Flags().String("id", "", "Task identifier (required)")
	asyncDeleteCmd.MarkFlagRequired("id")
	asyncCmd.AddCommand(asyncGetCmd)
	asyncCmd.AddCommand(asyncDeleteCmd)

	// Add version subcommands
	versionCheckCmd.Flags().Bool("pre-release", false, "Include pre-release versions")
	versionUpdateCmd.Flags().Bool("pre-release", false, "Include pre-release versions")
	versionCmd.AddCommand(versionShowCmd)
	versionCmd.AddCommand(versionCheckCmd)
	versionCmd.AddCommand(versionUpdateCmd)

	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(secretCmd)
	rootCmd.AddCommand(asyncCmd)
	rootCmd.AddCommand(versionCmd)
	// Add hidden commands (needed internally but not shown to users)
	rootCmd.AddCommand(daemonCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
