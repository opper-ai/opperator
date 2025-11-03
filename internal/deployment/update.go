package deployment

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	// Step 1: Build Linux binary
	var buildErr error
	binaryPath := ""

	err = spinner.New().
		Title("Building opperator for Linux...").
		Style(spinnerStyle).
		Action(func() {
			// Create temp directory for build
			tmpDir, err := os.MkdirTemp("", "opperator-build-")
			if err != nil {
				buildErr = fmt.Errorf("failed to create temp dir: %w", err)
				return
			}

			binaryPath = filepath.Join(tmpDir, "opperator")

			// Build for Linux
			cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/app")
			cmd.Env = append(os.Environ(),
				"GOOS=linux",
				"GOARCH=amd64",
				"CGO_ENABLED=0",
			)

			output, err := cmd.CombinedOutput()
			if err != nil {
				buildErr = fmt.Errorf("build failed: %w\n%s", err, string(output))
				return
			}
		}).
		Run()

	if err != nil {
		return err
	}
	if buildErr != nil {
		return buildErr
	}

	defer os.RemoveAll(filepath.Dir(binaryPath))

	fmt.Println("✓ Binary built for Linux")

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

	// Step 3: Transfer binary and restart daemon
	var updateErr error

	err = spinner.New().
		Title("Uploading new binary and restarting daemon...").
		Style(spinnerStyle).
		Action(func() {
			// Give it extra time for transfer and restart
			time.Sleep(2 * time.Second)

			provisioner, err := NewProvisioner(serverIP, sshKey)
			if err != nil {
				updateErr = fmt.Errorf("failed to connect to server: %w", err)
				return
			}
			defer provisioner.Close()

			// Read the new binary
			binaryData, err := os.ReadFile(binaryPath)
			if err != nil {
				updateErr = fmt.Errorf("failed to read binary: %w", err)
				return
			}

			// Stop the daemon
			if err := provisioner.runCommand("systemctl stop opperator"); err != nil {
				updateErr = fmt.Errorf("failed to stop daemon: %w", err)
				return
			}

			// Backup old binary
			if err := provisioner.runCommand("cp /opt/opperator/opperator /opt/opperator/opperator.bak"); err != nil {
				// Non-fatal, continue
			}

			// Upload new binary
			if err := provisioner.uploadFile(binaryData, "/opt/opperator/opperator"); err != nil {
				updateErr = fmt.Errorf("failed to upload binary: %w", err)
				return
			}

			// Make it executable
			if err := provisioner.runCommand("chmod +x /opt/opperator/opperator"); err != nil {
				updateErr = fmt.Errorf("failed to make binary executable: %w", err)
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
