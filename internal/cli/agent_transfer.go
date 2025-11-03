package cli

import (
	"fmt"
	"os"

	"opperator/config"
	"opperator/internal/agent"
	"opperator/internal/ipc"
)

// MoveAgent moves an agent from the current daemon to a target daemon
func MoveAgent(agentName, toDaemon string, force, noStart bool) error {
	// Validate agent name
	if agentName == "" {
		return fmt.Errorf("agent name is required")
	}

	if toDaemon == "" {
		return fmt.Errorf("target daemon name is required")
	}

	// Load daemon registry
	registry, err := config.LoadDaemonRegistry()
	if err != nil {
		return fmt.Errorf("failed to load daemon registry: %w", err)
	}

	// Get target daemon config
	targetDaemon, err := registry.GetDaemon(toDaemon)
	if err != nil {
		return fmt.Errorf("target daemon '%s' not found: %w", toDaemon, err)
	}

	if !targetDaemon.Enabled {
		return fmt.Errorf("target daemon '%s' is disabled", toDaemon)
	}

	// Find which daemon has the agent (check local first, then remotes)
	var sourceDaemon *config.DaemonConfig

	// Check local daemon first
	localConfig, err := config.GetConfigFile()
	if err == nil {
		localAgentConfig, err := agent.LoadConfig(localConfig)
		if err == nil {
			for _, a := range localAgentConfig.Agents {
				if a.Name == agentName {
					sourceDaemon = &config.DaemonConfig{
						Name:    "local",
						Address: "", // local doesn't need IPC
						Enabled: true,
					}
					break
				}
			}
		}
	}

	// If not found locally, check enabled remote daemons
	if sourceDaemon == nil {
		for _, d := range registry.Daemons {
			if !d.Enabled || d.Name == toDaemon {
				continue
			}

			client, err := ipc.NewClientWithAuth(d.Address, d.AuthToken)
			if err != nil {
				continue // Skip unreachable daemons
			}

			agents, err := client.ListAgents()
			client.Close()

			if err != nil {
				continue
			}

			for _, a := range agents {
				if a.Name == agentName {
					sourceDaemon = &d
					break
				}
			}

			if sourceDaemon != nil {
				break
			}
		}
	}

	if sourceDaemon == nil {
		return fmt.Errorf("agent '%s' not found on any enabled daemon", agentName)
	}

	// Step 1: Package the agent from source with wizard
	var pkg *agent.AgentPackage
	var wasRunning bool

	if sourceDaemon.Name == "local" {
		// Package from local with wizard
		localConfig, err := config.GetConfigFile()
		if err != nil {
			return fmt.Errorf("failed to get local config: %w", err)
		}

		// Check if agent is running
		socketPath, err := config.GetSocketPath()
		if err == nil {
			localClient, err := ipc.NewClient(socketPath)
			if err == nil {
				agents, err := localClient.ListAgents()
				localClient.Close()
				if err == nil {
					for _, a := range agents {
						if a.Name == agentName && string(a.Status) == "running" {
							wasRunning = true
							break
						}
					}
				}
			}
		}

		// Use wizard to package agent
		pkg, err = agent.PackageAgentWithWizard(agentName, sourceDaemon.Name, toDaemon, localConfig, wasRunning)
		if err != nil {
			return fmt.Errorf("failed to package agent: %w", err)
		}

		fmt.Println("✓ Packaged agent from local daemon")
	} else {
		// Package from remote daemon
		sourceClient, err := ipc.NewClientWithAuth(sourceDaemon.Address, sourceDaemon.AuthToken)
		if err != nil {
			return fmt.Errorf("failed to connect to source daemon: %w", err)
		}
		defer sourceClient.Close()

		pkg, err = sourceClient.PackageAgent(agentName)
		if err != nil {
			return fmt.Errorf("failed to package agent from remote: %w", err)
		}

		wasRunning = pkg.WasRunning
		fmt.Printf("✓ Packaged agent from '%s' daemon\n", sourceDaemon.Name)
	}

	// Step 2: Send to destination daemon
	if toDaemon == "local" {
		// Destination is local - unpackage directly
		localConfig, err := config.GetConfigFile()
		if err != nil {
			return fmt.Errorf("failed to get local config: %w", err)
		}

		if force {
			if err := agent.OverwriteAgent(pkg, localConfig); err != nil {
				return fmt.Errorf("failed to overwrite agent on local: %w", err)
			}
		} else {
			if err := agent.UnpackageAgent(pkg, localConfig); err != nil {
				return fmt.Errorf("failed to unpackage agent to local: %w", err)
			}
		}

		// Reload config so daemon picks up the new agent
		socketPath, err := config.GetSocketPath()
		if err == nil {
			localClient, err := ipc.NewClient(socketPath)
			if err == nil {
				if err := localClient.ReloadConfig(); err != nil {
					fmt.Printf("Warning: failed to reload config: %v\n", err)
				}
				localClient.Close()
			}
		}

		// Start agent if needed
		if !noStart && wasRunning {
			socketPath, err := config.GetSocketPath()
			if err == nil {
				localClient, err := ipc.NewClient(socketPath)
				if err == nil {
					localClient.StartAgent(agentName)
					localClient.Close()
				}
			}
		}

		fmt.Printf("✓ Agent received by '%s'\n", toDaemon)
	} else {
		// Destination is remote
		destClient, err := ipc.NewClientWithAuth(targetDaemon.Address, targetDaemon.AuthToken)
		if err != nil {
			return fmt.Errorf("failed to connect to destination daemon: %w", err)
		}
		defer destClient.Close()

		// Sync secrets to remote daemon
		if len(pkg.Secrets) > 0 {
			fmt.Printf("Syncing %d secret(s) to remote daemon...\n", len(pkg.Secrets))
			for name, value := range pkg.Secrets {
				if err := destClient.SetSecret(name, value); err != nil {
					fmt.Printf("Warning: failed to sync secret '%s': %v\n", name, err)
				}
			}
			fmt.Printf("✓ Secrets synced to '%s'\n", toDaemon)
		}

		// Send the agent package
		if err := destClient.ReceiveAgent(pkg, force, !noStart && wasRunning); err != nil {
			return fmt.Errorf("failed to send agent to destination: %w", err)
		}

		fmt.Printf("✓ Agent received by '%s'\n", toDaemon)
	}

	// Step 3: Delete from source
	if sourceDaemon.Name == "local" {
		// Delete from local
		if err := DeleteAgent(agentName, true, "local"); err != nil {
			fmt.Printf("Warning: failed to delete agent from source: %v\n", err)
			fmt.Println("You may need to manually delete the agent from the source daemon")
		} else {
			fmt.Printf("✓ Agent removed from '%s'\n", sourceDaemon.Name)
		}
	} else {
		// Delete from remote
		sourceClient, err := ipc.NewClientWithAuth(sourceDaemon.Address, sourceDaemon.AuthToken)
		if err == nil {
			if err := sourceClient.DeleteAgent(agentName); err != nil {
				fmt.Printf("Warning: failed to delete agent from source: %v\n", err)
			} else {
				fmt.Printf("✓ Agent removed from '%s'\n", sourceDaemon.Name)
			}
			sourceClient.Close()
		}
	}

	fmt.Printf("\n✔ Successfully moved agent '%s' to '%s'\n", agentName, toDaemon)

	return nil
}

// WhereIsAgent finds which daemon(s) have a specific agent
func WhereIsAgent(agentName string) error {
	if agentName == "" {
		return fmt.Errorf("agent name is required")
	}

	found := false

	// Check local daemon
	localConfig, err := config.GetConfigFile()
	if err == nil {
		localAgentConfig, err := agent.LoadConfig(localConfig)
		if err == nil {
			for _, a := range localAgentConfig.Agents {
				if a.Name == agentName {
					// Get status if daemon is running
					status := "unknown"
					socketPath, err := config.GetSocketPath()
					if err == nil {
						client, err := ipc.NewClient(socketPath)
						if err == nil {
							agents, err := client.ListAgents()
							client.Close()
							if err == nil {
								for _, agentInfo := range agents {
									if agentInfo.Name == agentName {
										status = string(agentInfo.Status)
										break
									}
								}
							}
						}
					}

					fmt.Printf("Agent '%s' found on: local (status: %s)\n", agentName, status)
					found = true
					break
				}
			}
		}
	}

	// Check remote daemons
	registry, err := config.LoadDaemonRegistry()
	if err == nil {
		for _, d := range registry.Daemons {
			if !d.Enabled || d.Name == "local" {
				continue // Skip disabled daemons and local (already checked above)
			}

			client, err := ipc.NewClientWithAuth(d.Address, d.AuthToken)
			if err != nil {
				continue // Skip unreachable daemons
			}

			agents, err := client.ListAgents()
			client.Close()

			if err != nil {
				continue
			}

			for _, a := range agents {
				if a.Name == agentName {
					fmt.Printf("Agent '%s' found on: %s (status: %s)\n", agentName, d.Name, a.Status)
					found = true
					break
				}
			}
		}
	}

	if !found {
		fmt.Printf("Agent '%s' not found on any daemon\n", agentName)
		os.Exit(1)
	}

	return nil
}
