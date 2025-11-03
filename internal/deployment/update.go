package deployment

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"opperator/config"
	"opperator/internal/credentials"
)

// Update updates the opperator binary on a cloud daemon
func Update(daemonName string) error {
	spinnerStyle := lipgloss.NewStyle().MarginLeft(2).Foreground(lipgloss.Color("#f7c0af"))
	ctx := context.Background()

	// Load daemon registry
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return fmt.Errorf("failed to load daemon registry: %w", err)
	}

	// Find daemon
	daemon, err := registry.GetDaemon(daemonName)
	if err != nil {
		return fmt.Errorf("daemon '%s' not found", daemonName)
	}

	// Check if it's a cloud daemon
	if daemon.Provider == "" || daemon.Provider == "local" {
		return fmt.Errorf("daemon '%s' is not a cloud deployment", daemonName)
	}

	fmt.Printf("\n🔄 Updating daemon '%s'\n\n", daemonName)

	// Step 2: Get server info and SSH credentials
	var serverIP string
	var sshKey string

	if daemon.Provider == "hetzner" {
		if daemon.HetznerServerID == 0 {
			return fmt.Errorf("no Hetzner server ID found for daemon '%s'", daemonName)
		}

		// Get Hetzner API key
		apiKey, err := credentials.GetSecret(hetznerAPIKeySecret)
		if err != nil || apiKey == "" {
			return fmt.Errorf("Hetzner API key not found. Cannot retrieve server info.")
		}

		// Get server info from Hetzner
		var serverInfo *ServerInfo
		var hetznerErr error

		err = spinner.New().
			Title("Fetching server information...").
			Style(spinnerStyle).
			Action(func() {
				client := NewHetznerClient(apiKey)
				serverInfo, hetznerErr = client.GetServer(ctx, daemon.HetznerServerID)
			}).
			Run()

		if err != nil {
			return err
		}
		if hetznerErr != nil {
			return fmt.Errorf("failed to get server info: %w", hetznerErr)
		}

		serverIP = serverInfo.PublicIP

		// Try to get SSH key from stored credentials
		sshKeyName := fmt.Sprintf("HETZNER_SSH_KEY_%s", daemonName)
		sshKey, err = credentials.GetSecret(sshKeyName)
		if err != nil || sshKey == "" {
			// SSH key not stored - prompt user for it
			fmt.Printf("✓ Server found: %s\n", serverIP)
			fmt.Println()
			fmt.Println("SSH private key needed to connect to the server.")
			fmt.Println("This is the key that was generated during deployment.")
			fmt.Println()
			fmt.Print("Enter the SSH private key path (or press Enter to skip): ")

			var keyPath string
			fmt.Scanln(&keyPath)

			if keyPath == "" {
				return fmt.Errorf("SSH key is required to update the daemon")
			}

			// Read the key file
			keyData, err := os.ReadFile(keyPath)
			if err != nil {
				return fmt.Errorf("failed to read SSH key from %s: %w", keyPath, err)
			}

			sshKey = string(keyData)

			// Offer to save it
			fmt.Print("\nSave this SSH key for future updates? (y/N): ")
			var save string
			fmt.Scanln(&save)

			if save == "y" || save == "Y" || save == "yes" {
				if err := credentials.SetSecret(sshKeyName, sshKey); err == nil {
					credentials.RegisterSecret(sshKeyName)
					fmt.Println("✓ SSH key saved")
				}
			}
		} else {
			fmt.Printf("✓ Server found: %s\n", serverIP)
		}
	} else {
		return fmt.Errorf("updating '%s' provider daemons is not yet supported", daemon.Provider)
	}

	// Step 3: Download latest release and restart daemon
	var updateErr error

	err = spinner.New().
		Title("Downloading latest release and restarting daemon...").
		Style(spinnerStyle).
		Action(func() {
			// Give it extra time for download and restart
			time.Sleep(2 * time.Second)

			provisioner, err := NewProvisioner(serverIP, sshKey)
			if err != nil {
				updateErr = fmt.Errorf("failed to connect to server: %w", err)
				return
			}
			defer provisioner.Close()

			// Stop the daemon
			if err := provisioner.runCommand("systemctl stop opperator"); err != nil {
				updateErr = fmt.Errorf("failed to stop daemon: %w", err)
				return
			}

			// Backup old binary
			if err := provisioner.runCommand("cp /opt/opperator/opperator /opt/opperator/opperator.bak"); err != nil {
				// Non-fatal, continue
			}

			// Download and install latest release from GitHub
			downloadCmd := `
				set -e
				cd /tmp

				# Fetch the latest release tag from GitHub API
				LATEST_VERSION=$(curl -sL https://api.github.com/repos/opper-ai/opperator/releases/latest | grep '"tag_name"' | sed -E 's/.*"tag_name": "([^"]+)".*/\1/')

				if [ -z "$LATEST_VERSION" ]; then
					echo "Failed to fetch latest version"
					exit 1
				fi

				echo "Downloading opperator version: $LATEST_VERSION"

				# Download the versioned Linux amd64 release
				curl -sL "https://github.com/opper-ai/opperator/releases/download/${LATEST_VERSION}/opperator-${LATEST_VERSION}-linux-amd64.tar.gz" -o opperator.tar.gz

				# Extract the binary (it's named with version, e.g., opperator-v0.1.0-linux-amd64)
				tar -xzf opperator.tar.gz

				# Find the extracted binary (should be opperator-{version}-linux-amd64)
				BINARY=$(find . -maxdepth 1 -name "opperator-*-linux-amd64" -type f | head -n1)

				if [ -z "$BINARY" ]; then
					echo "Failed to find extracted binary"
					exit 1
				fi

				# Move to /opt/opperator
				mv "$BINARY" /opt/opperator/opperator

				# Clean up
				rm opperator.tar.gz

				# Set executable permissions
				chmod +x /opt/opperator/opperator
				chown opperator:opperator /opt/opperator/opperator

				echo "Successfully updated to opperator $LATEST_VERSION"
			`

			if err := provisioner.runCommand(downloadCmd); err != nil {
				updateErr = fmt.Errorf("failed to download and install binary: %w", err)
				return
			}

			// Start the daemon
			if err := provisioner.runCommand("systemctl start opperator"); err != nil {
				updateErr = fmt.Errorf("failed to start daemon: %w", err)
				return
			}

			// Wait a moment for daemon to start
			time.Sleep(2 * time.Second)

			// Check if daemon is running
			if err := provisioner.runCommand("systemctl is-active opperator"); err != nil {
				updateErr = fmt.Errorf("daemon failed to start after update: %w", err)
				return
			}
		}).
		Run()

	if err != nil {
		return err
	}
	if updateErr != nil {
		return updateErr
	}

	// Print success message
	fg := lipgloss.Color("#dddddd")
	primary := lipgloss.Color("#f7c0af")
	baseStyle := lipgloss.NewStyle().Foreground(fg)
	highlightStyle := lipgloss.NewStyle().Foreground(primary).Bold(true)

	fmt.Println()
	fmt.Println(baseStyle.Render(" ✔︎ Update successful!"))
	fmt.Println()
	fmt.Print(baseStyle.Render(" Daemon '"))
	fmt.Print(highlightStyle.Render(daemonName))
	fmt.Print(baseStyle.Render("' has been updated and restarted."))
	fmt.Println()
	fmt.Println()

	return nil
}
