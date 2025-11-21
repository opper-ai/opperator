package publish

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"tui/llm"
)

// checkAgentLocation verifies that the agent is running on the local daemon.
// Returns an error if the agent is on a remote daemon.
func checkAgentLocation(agentName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agents, err := llm.ListAgents(ctx)
	if err != nil {
		// If we can't list agents, we'll proceed anyway (agent might not be running)
		return nil
	}

	for _, agent := range agents {
		if strings.EqualFold(agent.Name, agentName) {
			// Agent is running - check which daemon
			daemon := agent.Daemon
			if daemon == "" {
				daemon = "local" // Default to local for backward compatibility
			}

			if daemon != "local" {
				printRemoteDaemonError(agentName, daemon)
				return fmt.Errorf("agent on remote daemon")
			}

			// Agent is on local daemon, all good
			return nil
		}
	}

	// Agent not found in running agents - that's fine, it might not be running
	return nil
}

func printRemoteDaemonError(agentName, daemon string) {
	errorRed := lipgloss.Color("#bf5d47")
	primary := lipgloss.Color("#f7c0af")
	fgMuted := lipgloss.Color("#b3b3b3")
	secondary := lipgloss.Color("#3ccad7")

	// Error header
	errorLabel := lipgloss.NewStyle().Foreground(errorRed).Bold(true).MarginLeft(1).Render("Error:")
	agentStyled := lipgloss.NewStyle().Foreground(primary).Bold(true).Render(agentName)
	daemonStyled := lipgloss.NewStyle().Foreground(secondary).Bold(true).Render(daemon)
	message := lipgloss.NewStyle().Foreground(fgMuted).Render(
		fmt.Sprintf(" Agent %s is running on remote daemon %s", agentStyled, daemonStyled),
	)
	fmt.Println("\n" + errorLabel + message)

	// Instruction
	fmt.Println()
	instruction := lipgloss.NewStyle().Foreground(fgMuted).MarginLeft(1).Render("To publish, move it to local first:")
	fmt.Println(instruction)

	// Command
	command := lipgloss.NewStyle().Foreground(primary).MarginLeft(1).Render(
		fmt.Sprintf("  op agent move %s --to=local", agentName),
	)
	fmt.Println(command)
	fmt.Println()
}
