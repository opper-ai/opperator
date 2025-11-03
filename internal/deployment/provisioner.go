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
ExecStart=/opt/opperator/opperator daemon start
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
	// Step 1: Create user and directories
	if err := p.runCommand("useradd -m -s /bin/bash opperator || true"); err != nil {
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

	// Step 4: Configure firewall
	if err := p.configureFirewall(); err != nil {
		return fmt.Errorf("configure firewall: %w", err)
	}

	// Step 5: Create systemd service
	if err := p.createSystemdService(authToken); err != nil {
		return fmt.Errorf("create systemd service: %w", err)
	}

	// Step 6: Start daemon
	if err := p.startDaemon(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	return nil
}

// installSystemDependencies installs required system packages
func (p *Provisioner) installSystemDependencies() error {
	fmt.Println("Installing system dependencies (python3-venv, python3-pip)...")

	// Update package lists
	if err := p.runCommand("apt-get update -qq"); err != nil {
		return fmt.Errorf("update package lists: %w", err)
	}

	// Install Python and venv support
	// -y auto-confirms, -qq quiet output
	if err := p.runCommand("DEBIAN_FRONTEND=noninteractive apt-get install -y -qq python3-venv python3-pip"); err != nil {
		return fmt.Errorf("install python packages: %w", err)
	}

	fmt.Println("System dependencies installed successfully")
	return nil
}

// uploadBinary builds the opperator binary for Linux and uploads it
func (p *Provisioner) uploadBinary() error {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	// Build binary for Linux
	binaryPath := filepath.Join(cwd, "opperator-linux")
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

	// Read binary
	binaryData, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("read binary: %w", err)
	}

	// Upload via SFTP
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
