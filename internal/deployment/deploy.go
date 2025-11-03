package deployment

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"opperator/config"
	"opperator/internal/credentials"
)

const hetznerAPIKeySecret = "HETZNER_API_KEY"

// Deploy runs the full deployment workflow
func Deploy() error {
	spinnerStyle := lipgloss.NewStyle().MarginLeft(2).Foreground(lipgloss.Color("#f7c0af"))
	ctx := context.Background()

	// Check if we already have a saved Hetzner API key
	existingKey, _ := credentials.GetSecret(hetznerAPIKeySecret)
	hasExistingKey := existingKey != ""

	// First, get daemon name and API key (or use existing)
	input, err := RunWizardPartOne(hasExistingKey)
	if err != nil {
		return err
	}

	// If we have an existing key and user didn't provide one, use the existing key
	if input.APIKey == "" && existingKey != "" {
		input.APIKey = existingKey
		fmt.Println("\nUsing saved Hetzner API key")
	}

	// Validate API key early
	var validateErr error
	err = spinner.New().
		Title("Validating Hetzner API key...").
		Style(spinnerStyle).
		Action(func() {
			hetznerClient := NewHetznerClient(input.APIKey)
			validateErr = hetznerClient.ValidateAPIKey(ctx)
		}).
		Run()
	if err != nil {
		return err
	}
	if validateErr != nil {
		return fmt.Errorf("API key validation failed: %w", validateErr)
	}

	// Save the API key globally now that it's validated
	if err := credentials.SetSecret(hetznerAPIKeySecret, input.APIKey); err != nil {
		// Non-fatal - just warn the user
		fmt.Printf("\nWarning: Failed to store Hetzner API key: %v\n", err)
	} else {
		if err := credentials.RegisterSecret(hetznerAPIKeySecret); err != nil {
			fmt.Printf("\nWarning: Failed to register secret: %v\n", err)
		}
	}

	// Fetch server types and locations from Hetzner API
	var serverTypes []ServerTypeOption
	var locations []LocationOption
	var fetchErr error

	err = spinner.New().
		Title("Fetching server options from Hetzner...").
		Style(spinnerStyle).
		Action(func() {
			client := NewHetznerClient(input.APIKey)
			serverTypes, fetchErr = client.GetServerTypes(ctx)
			if fetchErr != nil {
				return
			}
			locations, fetchErr = client.GetLocations(ctx)
		}).
		Run()

	if err != nil {
		return err
	}
	if fetchErr != nil {
		return fmt.Errorf("failed to fetch server options from Hetzner: %w", fetchErr)
	}

	// Run second part of wizard with dynamic options
	input, err = RunWizardPartTwo(input, serverTypes, locations)
	if err != nil {
		return err
	}

	// Track created resources for cleanup on failure
	var serverInfo *ServerInfo
	var hetznerClient *HetznerClient

	// Cleanup function to delete server if deployment fails
	cleanup := func() {
		if serverInfo != nil && hetznerClient != nil {
			fg := lipgloss.Color("#dddddd")
			baseStyle := lipgloss.NewStyle().Foreground(fg)
			fmt.Println()
			fmt.Println(baseStyle.Render(" 🧹 Cleaning up..."))

			cleanupErr := spinner.New().
				Title(fmt.Sprintf("Deleting server %s...", serverInfo.Name)).
				Style(spinnerStyle).
				Action(func() {
					_ = hetznerClient.DeleteServer(ctx, serverInfo.ID)
				}).
				Run()

			if cleanupErr == nil {
				fmt.Println(baseStyle.Render(" ✓ Server deleted"))
			}
		}
	}

	// Step 1: Generate auth token
	var authToken string
	var tokenErr error
	err = spinner.New().
		Title("Generating authentication token...").
		Style(spinnerStyle).
		Action(func() {
			authToken, tokenErr = GenerateAuthToken()
		}).
		Run()
	if err != nil {
		return err
	}
	if tokenErr != nil {
		return fmt.Errorf("failed to generate auth token: %w", tokenErr)
	}

	// Step 2: Create Hetzner server
	var hetznerErr error
	hetznerClient = NewHetznerClient(input.APIKey)

	err = spinner.New().
		Title(fmt.Sprintf("Creating Hetzner %s server in %s...", input.ServerType, input.Location)).
		Style(spinnerStyle).
		Action(func() {
			serverInfo, hetznerErr = hetznerClient.CreateServer(ctx, input.Name, input.ServerType, input.Location)
		}).
		Run()
	if err != nil {
		return err
	}
	if hetznerErr != nil {
		return fmt.Errorf("failed to create server: %w", hetznerErr)
	}

	// Server created - ensure cleanup on failure from this point
	defer func() {
		if r := recover(); r != nil {
			cleanup()
			panic(r)
		}
	}()

	fmt.Printf("\n✓ Server created: %s (%s)\n", serverInfo.Name, serverInfo.PublicIP)
	fmt.Printf("  Type: %s\n", serverInfo.Type)
	fmt.Printf("  Location: %s\n", serverInfo.Location)
	fmt.Println()

	// Step 3: Provision the server
	var provisionErr error
	err = spinner.New().
		Title("Provisioning server (installing opperator)...").
		Style(spinnerStyle).
		Action(func() {
			// Wait a bit for SSH to be ready
			time.Sleep(10 * time.Second)

			provisioner, err := NewProvisioner(serverInfo.PublicIP, serverInfo.PrivateKey)
			if err != nil {
				provisionErr = err
				return
			}
			defer provisioner.Close()

			provisionErr = provisioner.Provision(ctx, authToken)
		}).
		Run()
	if err != nil {
		cleanup()
		return err
	}
	if provisionErr != nil {
		cleanup()
		return fmt.Errorf("failed to provision server: %w", provisionErr)
	}

	// Step 4: Register daemon in local config
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		cleanup()
		return fmt.Errorf("failed to load daemon registry: %w", err)
	}

	daemon := config.DaemonConfig{
		Name:            input.Name,
		Address:         fmt.Sprintf("tcp://%s:%s", serverInfo.PublicIP, opperatorPort),
		AuthToken:       authToken,
		Enabled:         true,
		Provider:        "hetzner",
		HetznerServerID: serverInfo.ID,
	}

	if err := registry.AddDaemon(daemon); err != nil {
		cleanup()
		return fmt.Errorf("failed to add daemon: %w", err)
	}

	if err := config.SaveDaemonRegistry(registry); err != nil {
		cleanup()
		return fmt.Errorf("failed to save daemon registry: %w", err)
	}

	// Step 5: Store SSH key for future updates
	sshKeySecretName := fmt.Sprintf("HETZNER_SSH_KEY_%s", input.Name)
	if err := credentials.SetSecret(sshKeySecretName, serverInfo.PrivateKey); err != nil {
		// Non-fatal - just warn the user
		fmt.Printf("\nWarning: Failed to store SSH key: %v\n", err)
		fmt.Printf("You may need to manually update this daemon in the future.\n")
	} else {
		if err := credentials.RegisterSecret(sshKeySecretName); err != nil {
			fmt.Printf("\nWarning: Failed to register SSH key secret: %v\n", err)
		}
	}

	// Print success message
	fg := lipgloss.Color("#dddddd")
	primary := lipgloss.Color("#f7c0af")
	baseStyle := lipgloss.NewStyle().Foreground(fg)
	highlightStyle := lipgloss.NewStyle().Foreground(primary).Bold(true)

	fmt.Println()
	fmt.Println(baseStyle.Render(" ✔︎ Deployment successful!"))
	fmt.Println()
	fmt.Print(baseStyle.Render(" Daemon '"))
	fmt.Print(highlightStyle.Render(input.Name))
	fmt.Print(baseStyle.Render("' is now running at "))
	fmt.Print(highlightStyle.Render(fmt.Sprintf("%s:%s", serverInfo.PublicIP, opperatorPort)))
	fmt.Println()
	fmt.Println()
	fmt.Print(baseStyle.Render(" Test the connection with: "))
	fmt.Print(highlightStyle.Render(fmt.Sprintf("op daemon test %s", input.Name)))
	fmt.Println()
	fmt.Println()

	return nil
}

// Destroy deletes a deployed daemon and its server
func Destroy(daemonName string, force bool) error {
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

	// Check if it's a Hetzner daemon
	if daemon.Provider != "hetzner" {
		return fmt.Errorf("daemon '%s' is not a Hetzner deployment (provider: %s)", daemonName, daemon.Provider)
	}

	if daemon.HetznerServerID == 0 {
		return fmt.Errorf("daemon '%s' has no Hetzner server ID", daemonName)
	}

	// Confirm destruction
	if !force {
		fg := lipgloss.Color("#dddddd")
		errorColor := lipgloss.Color("#bf5d47")
		baseStyle := lipgloss.NewStyle().Foreground(fg)
		errorStyle := lipgloss.NewStyle().Foreground(errorColor).Bold(true)

		fmt.Println()
		fmt.Println(errorStyle.Render(" ⚠️  WARNING: This will DELETE the server permanently!"))
		fmt.Println(baseStyle.Render("    All data will be lost and billing will stop."))
		fmt.Println()
		fmt.Printf("    Daemon: %s\n", daemonName)
		fmt.Printf("    Server ID: %d\n", daemon.HetznerServerID)
		fmt.Printf("    Address: %s\n", daemon.Address)
		fmt.Println()
		fmt.Print(baseStyle.Render(" Type 'yes' to confirm: "))

		var response string
		fmt.Scanln(&response)
		if response != "yes" {
			fmt.Println("\nDestroy cancelled.")
			return nil
		}
	}

	// Try to get Hetzner API key from secrets manager (global key)
	apiKey, err := credentials.GetSecret(hetznerAPIKeySecret)

	// If not found in secrets, prompt the user
	if err != nil || apiKey == "" {
		fg := lipgloss.Color("#dddddd")
		baseStyle := lipgloss.NewStyle().Foreground(fg)

		fmt.Println()
		fmt.Println(baseStyle.Render(" Hetzner API key needed to delete the server."))
		fmt.Print(baseStyle.Render(" Enter Hetzner API key: "))

		var inputKey string
		fmt.Scanln(&inputKey)

		if inputKey == "" {
			return fmt.Errorf("API key is required to delete server")
		}

		apiKey = inputKey
	}

	spinnerStyle := lipgloss.NewStyle().MarginLeft(2).Foreground(lipgloss.Color("#f7c0af"))
	ctx := context.Background()

	// Validate API key before attempting deletion
	var validateErr error
	err = spinner.New().
		Title("Validating Hetzner API key...").
		Style(spinnerStyle).
		Action(func() {
			client := NewHetznerClient(apiKey)
			validateErr = client.ValidateAPIKey(ctx)
		}).
		Run()
	if err != nil {
		return err
	}
	if validateErr != nil {
		return fmt.Errorf("API key validation failed: %w\nCannot delete server without valid API key", validateErr)
	}

	// Delete server
	var deleteErr error
	err = spinner.New().
		Title(fmt.Sprintf("Deleting server (ID: %d)...", daemon.HetznerServerID)).
		Style(spinnerStyle).
		Action(func() {
			client := NewHetznerClient(apiKey)
			deleteErr = client.DeleteServer(ctx, daemon.HetznerServerID)
		}).
		Run()
	if err != nil {
		return err
	}
	if deleteErr != nil {
		return fmt.Errorf("failed to delete server: %w", deleteErr)
	}

	// Remove from registry
	if err := registry.RemoveDaemon(daemonName); err != nil {
		return fmt.Errorf("failed to remove daemon from registry: %w", err)
	}

	if err := config.SaveDaemonRegistry(registry); err != nil {
		return fmt.Errorf("failed to save daemon registry: %w", err)
	}

	// Note: We keep the Hetzner API key in secrets manager
	// The user might want to deploy other servers or redeploy later

	// Print success message
	fg := lipgloss.Color("#dddddd")
	primary := lipgloss.Color("#f7c0af")
	baseStyle := lipgloss.NewStyle().Foreground(fg)
	highlightStyle := lipgloss.NewStyle().Foreground(primary).Bold(true)

	fmt.Println()
	fmt.Println(baseStyle.Render(" ✔︎ Server deleted successfully!"))
	fmt.Println()
	fmt.Print(baseStyle.Render(" Daemon '"))
	fmt.Print(highlightStyle.Render(daemonName))
	fmt.Print(baseStyle.Render("' has been removed."))
	fmt.Println()
	fmt.Println()

	return nil
}
