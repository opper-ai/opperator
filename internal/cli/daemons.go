package cli

import (
	"fmt"
	"net"
	"strings"
	"time"

	"opperator/config"
	"opperator/internal/ipc"
)

// AddDaemon adds a new daemon to the registry
func AddDaemon(name, address, authToken string, enabled bool) error {
	// Validate inputs
	if name == "" {
		return fmt.Errorf("daemon name cannot be empty")
	}

	if err := config.ValidateAddress(address); err != nil {
		return err
	}

	// Load existing registry
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return fmt.Errorf("failed to load daemon registry: %w", err)
	}

	// Check if daemon already exists
	existing, _ := registry.GetDaemon(name)
	if existing != nil {
		fmt.Printf("Daemon '%s' already exists. Updating...\n", name)
	}

	// Create daemon config
	daemon := config.DaemonConfig{
		Name:      name,
		Address:   address,
		AuthToken: authToken,
		Enabled:   enabled,
	}

	// Add or update daemon
	if err := registry.AddDaemon(daemon); err != nil {
		return fmt.Errorf("failed to add daemon: %w", err)
	}

	// Save registry
	if err := config.SaveDaemonRegistry(registry); err != nil {
		return fmt.Errorf("failed to save daemon registry: %w", err)
	}

	registryPath, _ := config.GetDaemonRegistryPath()

	if existing != nil {
		fmt.Printf("✓ Updated daemon '%s'\n", name)
	} else {
		fmt.Printf("✓ Added daemon '%s'\n", name)
	}

	fmt.Printf("  Address: %s\n", address)
	if authToken != "" {
		fmt.Printf("  Auth: configured\n")
	}
	fmt.Printf("  Enabled: %v\n", enabled)
	fmt.Printf("  Registry: %s\n", registryPath)

	return nil
}

// ListDaemons lists all configured daemons
func ListDaemons() error {
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return fmt.Errorf("failed to load daemon registry: %w", err)
	}

	if len(registry.Daemons) == 0 {
		fmt.Println("No daemons configured")
		registryPath, _ := config.GetDaemonRegistryPath()
		fmt.Printf("\nAdd a daemon with: op daemon add <name> <address>\n")
		fmt.Printf("Example: op daemon add production tcp://my-server.com:9999\n")
		fmt.Printf("Config file: %s\n", registryPath)
		return nil
	}

	fmt.Printf("%-15s %-10s %-40s %s\n", "NAME", "STATUS", "ADDRESS", "AUTH")
	fmt.Printf("%-15s %-10s %-40s %s\n", "----", "------", "-------", "----")

	for _, d := range registry.Daemons {
		status := "disabled"
		if d.Enabled {
			status = "enabled"
		}

		auth := "no"
		if d.AuthToken != "" {
			auth = "yes"
		}

		fmt.Printf("%-15s %-10s %-40s %s\n", d.Name, status, d.Address, auth)
	}

	fmt.Printf("\nTotal: %d daemon(s)\n", len(registry.Daemons))
	return nil
}

// RemoveDaemon removes a daemon from the registry
func RemoveDaemon(name string, force bool) error {
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return fmt.Errorf("failed to load daemon registry: %w", err)
	}

	// Check if daemon exists
	daemon, err := registry.GetDaemon(name)
	if err != nil {
		return err
	}

	// Confirm unless --force is used
	if !force {
		fmt.Printf("Remove daemon '%s' (%s)? (y/N): ", name, daemon.Address)
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Remove daemon
	if err := registry.RemoveDaemon(name); err != nil {
		return err
	}

	// Save registry
	if err := config.SaveDaemonRegistry(registry); err != nil {
		return fmt.Errorf("failed to save daemon registry: %w", err)
	}

	fmt.Printf("✓ Removed daemon '%s'\n", name)
	return nil
}

// TestDaemon tests connectivity to a daemon
func TestDaemon(name string) error {
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return fmt.Errorf("failed to load daemon registry: %w", err)
	}

	daemon, err := registry.GetDaemon(name)
	if err != nil {
		return err
	}

	fmt.Printf("Testing connection to '%s'...\n", name)
	fmt.Printf("  Address: %s\n", daemon.Address)

	// Parse address
	var network, address string
	if strings.HasPrefix(daemon.Address, "unix://") {
		network = "unix"
		address = strings.TrimPrefix(daemon.Address, "unix://")
	} else if strings.HasPrefix(daemon.Address, "tcp://") {
		network = "tcp"
		address = strings.TrimPrefix(daemon.Address, "tcp://")
	} else {
		return fmt.Errorf("unsupported address scheme: %s", daemon.Address)
	}

	// Try to connect with timeout
	conn, err := net.DialTimeout(network, address, 5*time.Second)
	if err != nil {
		fmt.Printf("✗ Connection failed: %v\n", err)
		fmt.Printf("\nTroubleshooting:\n")
		fmt.Printf("  - Check if the daemon is running\n")
		fmt.Printf("  - Verify the address is correct\n")
		fmt.Printf("  - Check firewall settings (for TCP connections)\n")
		if network == "unix" {
			fmt.Printf("  - Ensure the socket file exists: %s\n", address)
		}
		return fmt.Errorf("connection test failed")
	}
	defer conn.Close()

	fmt.Printf("✓ Connection successful!\n")
	fmt.Printf("  Remote: %s\n", conn.RemoteAddr())

	if !daemon.Enabled {
		fmt.Printf("\nNote: Daemon is currently disabled. Enable with:\n")
		fmt.Printf("  op daemon add %s %s --enabled\n", name, daemon.Address)
	}

	// For TCP connections, test full authentication and agent listing
	if network == "tcp" {
		fmt.Printf("\nTesting full IPC connection (with auth)...\n")
		client, err := ipc.NewClientWithAuth(daemon.Address, daemon.AuthToken)
		if err != nil {
			fmt.Printf("✗ IPC connection failed: %v\n", err)
			return fmt.Errorf("IPC test failed")
		}
		defer client.Close()

		agents, err := client.ListAgents()
		if err != nil {
			fmt.Printf("✗ Failed to list agents: %v\n", err)
			return fmt.Errorf("agent listing failed")
		}

		fmt.Printf("✓ Authentication successful!\n")
		fmt.Printf("✓ Agent listing works (%d agent(s) configured)\n", len(agents))
	}

	return nil
}
