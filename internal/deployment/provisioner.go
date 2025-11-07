package deployment

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"opperator/version"
)

const (
	opperatorPort = "9999"
	systemdServiceTemplate = `[Unit]
Description=Opperator Agent Daemon
After=network.target

[Service]
Type=simple
User=opperator
Group=opperator
WorkingDirectory=/var/lib/opperator
ExecStart=/usr/bin/dbus-run-session -- /opt/opperator/opperator-wrapper.sh
Restart=on-failure
RestartSec=5s

# Environment variables
Environment="HOME=/var/lib/opperator"
Environment="OPPERATOR_TCP_PORT=%s"
Environment="OPPERATOR_AUTH_TOKEN=%s"
Environment="OPPERATOR_CONFIG_DIR=/var/lib/opperator"
Environment="OPPERATOR_LOGS_DIR=/var/log/opperator"

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/opperator /var/log/opperator

[Install]
WantedBy=multi-user.target
`
)

// Provisioner handles SSH-based provisioning of opperator daemon
type Provisioner struct {
	sshClient *ssh.Client
	host      string
	port      string
}

// NewProvisioner creates a new provisioner
func NewProvisioner(host string, privateKey string) (*Provisioner, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: Improve this
		Timeout:         30 * time.Second,
	}

	// Retry connection a few times (server might still be booting)
	var client *ssh.Client
	for i := 0; i < 10; i++ {
		client, err = ssh.Dial("tcp", host+":22", config)
		if err == nil {
			break
		}
		time.Sleep(5 * time.Second)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect via SSH: %w", err)
	}

	return &Provisioner{
		sshClient: client,
		host:      host,
		port:      "22",
	}, nil
}

// Close closes the SSH connection
func (p *Provisioner) Close() error {
	if p.sshClient != nil {
		return p.sshClient.Close()
	}
	return nil
}

// Provision sets up the opperator daemon on the remote server
func (p *Provisioner) Provision(ctx context.Context, authToken string) error {
	// Step 1: Create user with home directory at /var/lib/opperator
	if err := p.runCommand("useradd -d /var/lib/opperator -m -s /bin/bash opperator || true"); err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	// Create all necessary directories
	if err := p.runCommand("mkdir -p /opt/opperator /var/lib/opperator /var/log/opperator"); err != nil {
		return fmt.Errorf("create directories: %w", err)
	}

	// Create config directory structure in the opperator user's home
	if err := p.runCommand("mkdir -p /var/lib/opperator/.config/opperator/logs"); err != nil {
		return fmt.Errorf("create config directories: %w", err)
	}

	// Set ownership for all directories
	if err := p.runCommand("chown -R opperator:opperator /opt/opperator /var/lib/opperator /var/log/opperator"); err != nil {
		return fmt.Errorf("set ownership: %w", err)
	}

	// Ensure proper permissions on home directory
	if err := p.runCommand("chmod 755 /var/lib/opperator"); err != nil {
		return fmt.Errorf("set home permissions: %w", err)
	}

	// Step 2: Install system dependencies
	if err := p.installSystemDependencies(); err != nil {
		return fmt.Errorf("install system dependencies: %w", err)
	}

	// Step 3: Build and upload binary
	if err := p.uploadBinary(); err != nil {
		return fmt.Errorf("upload binary: %w", err)
	}

	// Step 4: Create wrapper script for keyring support
	if err := p.createWrapperScript(); err != nil {
		return fmt.Errorf("create wrapper script: %w", err)
	}

	// Step 5: Configure firewall
	if err := p.configureFirewall(); err != nil {
		return fmt.Errorf("configure firewall: %w", err)
	}

	// Step 6: Create systemd service
	if err := p.createSystemdService(authToken); err != nil {
		return fmt.Errorf("create systemd service: %w", err)
	}

	// Step 7: Initialize keyring
	if err := p.initializeKeyring(); err != nil {
		return fmt.Errorf("initialize keyring: %w", err)
	}

	// Step 8: Start daemon
	if err := p.startDaemon(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	return nil
}

// installSystemDependencies installs required system packages
func (p *Provisioner) installSystemDependencies() error {
	fmt.Println("Installing system dependencies (python3-venv, python3-pip, gnome-keyring, dbus-x11, libsecret-tools)...")

	// Update package lists
	if err := p.runCommand("apt-get update -qq"); err != nil {
		return fmt.Errorf("update package lists: %w", err)
	}

	// Install Python, venv support, and keyring dependencies
	// -y auto-confirms, -qq quiet output
	if err := p.runCommand("DEBIAN_FRONTEND=noninteractive apt-get install -y -qq python3-venv python3-pip gnome-keyring dbus-x11 libsecret-tools"); err != nil {
		return fmt.Errorf("install packages: %w", err)
	}

	fmt.Println("System dependencies installed successfully")
	return nil
}

// uploadBinary uploads and installs the opperator binary
// If running a dev version, builds from local source and uploads via SFTP
// If running a release version, downloads from GitHub releases on the server
func (p *Provisioner) uploadBinary() error {
	currentVersion := version.Get()

	// Check if this is a development version
	if currentVersion == "dev" {
		fmt.Println("Detected development version, building from local source...")
		return p.uploadBinaryFromSource()
	}

	// Production version: download from GitHub releases
	fmt.Printf("Deploying release version %s from GitHub...\n", currentVersion)
	return p.uploadBinaryFromGitHub()
}

// uploadBinaryFromSource builds locally and uploads via SFTP (for dev versions)
func (p *Provisioner) uploadBinaryFromSource() error {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	// Build binary for Linux
	binaryPath := filepath.Join(cwd, "opperator-linux")
	fmt.Println("Building opperator for Linux amd64...")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/app")
	cmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build binary: %w\n%s", err, string(output))
	}
	defer os.Remove(binaryPath)

	fmt.Println("✓ Binary built successfully")

	// Read binary
	binaryData, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("read binary: %w", err)
	}

	// Upload via SFTP
	fmt.Println("Uploading binary to remote server...")
	if err := p.uploadFile(binaryData, "/opt/opperator/opperator"); err != nil {
		return fmt.Errorf("upload file: %w", err)
	}

	// Set executable permissions
	if err := p.runCommand("chmod +x /opt/opperator/opperator"); err != nil {
		return fmt.Errorf("set permissions: %w", err)
	}

	if err := p.runCommand("chown opperator:opperator /opt/opperator/opperator"); err != nil {
		return fmt.Errorf("set ownership: %w", err)
	}

	fmt.Println("✓ Binary uploaded and configured")
	return nil
}

// uploadBinaryFromGitHub downloads release from GitHub on the server (for release versions)
func (p *Provisioner) uploadBinaryFromGitHub() error {
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

		echo "Successfully installed opperator $LATEST_VERSION"
	`

	if err := p.runCommand(downloadCmd); err != nil {
		return fmt.Errorf("download and install binary: %w", err)
	}

	return nil
}

// createWrapperScript creates a wrapper script that initializes gnome-keyring before starting opperator
func (p *Provisioner) createWrapperScript() error {
	wrapperScript := `#!/bin/bash
set -e

# Ensure HOME is set (required for keyring)
export HOME="${HOME:-/var/lib/opperator}"

# Start gnome-keyring-daemon with empty password unlock
# Use printf to provide TWO newlines (password + confirmation for creating new keyring)
# This creates an unlocked keyring that agents can use to store/retrieve secrets
printf "\n\n" | gnome-keyring-daemon --unlock --components=secrets --daemonize 2>/dev/null || {
    echo "Warning: Failed to start gnome-keyring-daemon, secrets may not work" >&2
}

# Wait a moment for daemon to be ready
sleep 0.5

# Start the actual Opperator daemon in foreground mode
exec /opt/opperator/opperator daemon start --foreground
`

	// Upload wrapper script
	if err := p.uploadFile([]byte(wrapperScript), "/opt/opperator/opperator-wrapper.sh"); err != nil {
		return fmt.Errorf("upload wrapper script: %w", err)
	}

	// Set executable permissions
	if err := p.runCommand("chmod +x /opt/opperator/opperator-wrapper.sh"); err != nil {
		return fmt.Errorf("set wrapper permissions: %w", err)
	}

	// Set ownership
	if err := p.runCommand("chown opperator:opperator /opt/opperator/opperator-wrapper.sh"); err != nil {
		return fmt.Errorf("set wrapper ownership: %w", err)
	}

	return nil
}

// configureFirewall configures UFW to allow opperator port
func (p *Provisioner) configureFirewall() error {
	// Check if UFW is installed
	if err := p.runCommand("which ufw"); err == nil {
		// UFW is installed, use it
		if err := p.runCommand(fmt.Sprintf("ufw allow %s/tcp", opperatorPort)); err != nil {
			return fmt.Errorf("allow port in UFW: %w", err)
		}
	} else {
		// Fall back to iptables
		if err := p.runCommand(fmt.Sprintf("iptables -A INPUT -p tcp --dport %s -j ACCEPT", opperatorPort)); err != nil {
			return fmt.Errorf("allow port in iptables: %w", err)
		}
	}

	return nil
}

// createSystemdService creates and enables the systemd service
func (p *Provisioner) createSystemdService(authToken string) error {
	serviceContent := fmt.Sprintf(systemdServiceTemplate, opperatorPort, authToken)

	// Upload service file
	if err := p.uploadFile([]byte(serviceContent), "/etc/systemd/system/opperator.service"); err != nil {
		return fmt.Errorf("upload service file: %w", err)
	}

	// Reload systemd
	if err := p.runCommand("systemctl daemon-reload"); err != nil {
		return fmt.Errorf("reload systemd: %w", err)
	}

	// Enable service
	if err := p.runCommand("systemctl enable opperator"); err != nil {
		return fmt.Errorf("enable service: %w", err)
	}

	return nil
}

// initializeKeyring initializes the gnome-keyring for the opperator user
// This creates the login.keyring file with an empty password for headless automation
func (p *Provisioner) initializeKeyring() error {
	initScript := `
		su - opperator -c '
			export HOME=/var/lib/opperator

			# Create keyring directories with proper permissions
			mkdir -p ~/.local/share/keyrings ~/.cache
			chmod 700 ~/.local/share/keyrings

			# Initialize keyring within a D-Bus session
			dbus-run-session -- bash -c "
				# Create keyring with empty password (two newlines: password + confirmation)
				# This allows the daemon to store/retrieve secrets without password prompts
				printf \"\\n\\n\" | gnome-keyring-daemon --unlock --components=secrets --daemonize 2>/dev/null
				sleep 1

				# Store a test secret to verify keyring is working
				echo -n \"initialized\" | secret-tool store --label=\"opperator-init\" service opperator username init 2>/dev/null || echo \"Warning: test secret storage failed\" >&2
			"
		'
	`

	if err := p.runCommand(initScript); err != nil {
		return fmt.Errorf("initialize keyring: %w", err)
	}

	return nil
}

// startDaemon starts the opperator daemon
func (p *Provisioner) startDaemon() error {
	if err := p.runCommand("systemctl start opperator"); err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	// Wait a bit and check status
	time.Sleep(2 * time.Second)

	if err := p.runCommand("systemctl is-active opperator"); err != nil {
		// Get logs for debugging
		logs, _ := p.runCommandOutput("journalctl -u opperator -n 50 --no-pager")
		return fmt.Errorf("service failed to start:\n%s", logs)
	}

	return nil
}

// runCommand executes a command via SSH
func (p *Provisioner) runCommand(cmd string) error {
	session, err := p.sshClient.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	var stderr bytes.Buffer
	session.Stderr = &stderr

	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}

	return nil
}

// runCommandOutput executes a command and returns its output
func (p *Provisioner) runCommandOutput(cmd string) (string, error) {
	session, err := p.sshClient.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	output, err := session.CombinedOutput(cmd)
	return string(output), err
}

// uploadFile uploads a file via SFTP
func (p *Provisioner) uploadFile(data []byte, remotePath string) error {
	// Create SFTP client
	sftpClient, err := sftp.NewClient(p.sshClient)
	if err != nil {
		return fmt.Errorf("failed to create SFTP client: %w", err)
	}
	defer sftpClient.Close()

	// Ensure remote directory exists
	remoteDir := filepath.Dir(remotePath)
	if err := sftpClient.MkdirAll(remoteDir); err != nil {
		return fmt.Errorf("failed to create remote directory: %w", err)
	}

	// Create remote file
	remoteFile, err := sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("failed to create remote file: %w", err)
	}
	defer remoteFile.Close()

	// Write data
	if _, err := remoteFile.Write(data); err != nil {
		return fmt.Errorf("failed to write to remote file: %w", err)
	}

	return nil
}

// GenerateAuthToken generates a secure random auth token
func GenerateAuthToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}
