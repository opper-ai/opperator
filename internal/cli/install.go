package cli

import (
	"opperator/internal/install"
	"opperator/internal/ipc"
)

// InstallOptions contains options for agent installation
type InstallOptions struct {
	CustomName string
	Force      bool
	NoStart    bool
}

// InstallAgent installs an agent from a GitHub repository
func InstallAgent(packageRef string, opts InstallOptions) error {
	// Install the agent
	result, err := install.InstallAgent(packageRef, install.InstallOptions{
		CustomName: opts.CustomName,
		Force:      opts.Force,
		NoStart:    opts.NoStart,
	})
	if err != nil {
		return err
	}

	// Reload daemon config so it knows about the new agent
	err = install.RunStepWithSpinner("Reloading daemon config...", func() error {
		client, err := ipc.NewClientFromRegistry("local")
		if err != nil {
			return err
		}
		defer client.Close()

		return client.ReloadConfig()
	})
	if err != nil {
		// Non-fatal - just warn the user
		install.PrintWarning("Failed to reload daemon config: " + err.Error())
	}

	// Auto-start if requested (handled here to avoid circular imports)
	if result.ShouldAutoStart {
		// Start with spinner
		err = install.RunStepWithSpinner("Starting agent...", func() error {
			return StartAgent(result.AgentName, "") // empty string for daemon means auto-detect
		})
		if err != nil {
			install.PrintWarning("Failed to start agent: " + err.Error())
			install.PrintInfo("You can start it manually with: op agent start " + result.AgentName)
		}
	}

	// Print success message
	install.PrintInstallSuccessMessage(
		result.AgentName,
		result.ShouldAutoStart && err == nil,
		result.ConfiguredSecrets,
	)

	return nil
}
