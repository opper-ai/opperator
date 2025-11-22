package install

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
)

// RunStepWithSpinner runs an action with a spinner and shows success/failure indicator
func RunStepWithSpinner(title string, action func() error) error {
	primary := lipgloss.Color("#f7c0af")
	fgMuted := lipgloss.Color("#b3b3b3")
	errorRed := lipgloss.Color("#bf5d47")

	var actionErr error
	spinnerAction := func() {
		actionErr = action()
	}

	_ = spinner.New().
		Title(title).
		Style(lipgloss.NewStyle().Foreground(primary).MarginLeft(1)).
		Action(spinnerAction).
		Run()

	if actionErr == nil {
		// Print success checkmark to preserve progress
		checkmark := lipgloss.NewStyle().Foreground(primary).MarginLeft(1).Render("✓")
		message := lipgloss.NewStyle().Foreground(fgMuted).Render(" " + strings.TrimSuffix(title, "..."))
		fmt.Println(checkmark + message)
	} else {
		// Print red X to indicate failure
		failmark := lipgloss.NewStyle().Foreground(errorRed).MarginLeft(1).Render("✗")
		message := lipgloss.NewStyle().Foreground(fgMuted).Render(" " + strings.TrimSuffix(title, "..."))
		fmt.Println(failmark + message)
	}

	return actionErr
}

// PrintError prints an error message in red
func PrintError(err error) {
	errorRed := lipgloss.Color("#bf5d47")
	fgMuted := lipgloss.Color("#b3b3b3")

	label := lipgloss.NewStyle().Foreground(errorRed).Bold(true).MarginLeft(1).Render("Error:")
	message := lipgloss.NewStyle().Foreground(fgMuted).Render(" " + err.Error())
	fmt.Println(label + message)
}

// PrintWarning prints a warning message in yellow
func PrintWarning(message string) {
	yellow := lipgloss.Color("#f1fa8c")
	fgMuted := lipgloss.Color("#b3b3b3")

	label := lipgloss.NewStyle().Foreground(yellow).Bold(true).MarginLeft(1).Render("Warning:")
	msg := lipgloss.NewStyle().Foreground(fgMuted).Render(" " + message)
	fmt.Println(label + msg)
}

// PrintInfo prints an informational message
func PrintInfo(message string) {
	yellow := lipgloss.Color("#f1fa8c")
	fgMuted := lipgloss.Color("#b3b3b3")

	fmt.Println() // Add newline before
	icon := lipgloss.NewStyle().Foreground(yellow).MarginLeft(1).Render("⚠")
	msg := lipgloss.NewStyle().Foreground(fgMuted).Render("  " + message)
	fmt.Println(icon + msg)
}

// PrintInstallSuccessMessage prints a success message after installation
func PrintInstallSuccessMessage(agentName string, autoStarted bool, configuredSecrets int) {
	primary := lipgloss.Color("#f7c0af")
	fgMuted := lipgloss.Color("#b3b3b3")
	cmdColor := lipgloss.Color("#3ccad7")

	fmt.Println()

	agentStyled := lipgloss.NewStyle().Foreground(primary).Bold(true).Render(agentName)
	message := lipgloss.NewStyle().Foreground(fgMuted).Render("Installed ")
	success := lipgloss.NewStyle().Foreground(fgMuted).Render(" successfully!")

	fmt.Printf("%s\n", lipgloss.NewStyle().MarginLeft(1).Render(message+agentStyled+success))

	if configuredSecrets > 0 {
		secretsMsg := lipgloss.NewStyle().Foreground(fgMuted).MarginLeft(1).Render(
			fmt.Sprintf("Configured %d secret(s)", configuredSecrets),
		)
		fmt.Println(secretsMsg)
	}

	if !autoStarted {
		fmt.Println()
		nextSteps := lipgloss.NewStyle().Foreground(primary).Bold(true).MarginLeft(1).Render("Next step:")
		fmt.Println(nextSteps)
		cmd := lipgloss.NewStyle().Foreground(cmdColor).MarginLeft(1).Render(
			fmt.Sprintf("  op agent start %s", agentName),
		)
		fmt.Println(cmd)
	} else {
		fmt.Println()
		verifySteps := lipgloss.NewStyle().Foreground(primary).Bold(true).MarginLeft(1).Render("Verify:")
		fmt.Println(verifySteps)

		listCmd := lipgloss.NewStyle().Foreground(cmdColor).MarginLeft(1).Render("  op agent list")
		fmt.Println(listCmd)

		logsCmd := lipgloss.NewStyle().Foreground(cmdColor).MarginLeft(1).Render(
			fmt.Sprintf("  op agent logs %s", agentName),
		)
		fmt.Println(logsCmd)
	}

	fmt.Println()
}

// printAgentInfo displays information about the agent being installed
func printAgentInfo(metadata *AgentMetadata) {
	primary := lipgloss.Color("#f7c0af")
	fgMuted := lipgloss.Color("#b3b3b3")

	fmt.Println()

	nameLabel := lipgloss.NewStyle().Foreground(primary).Bold(true).MarginLeft(1).Render("Agent:")
	nameValue := lipgloss.NewStyle().Foreground(fgMuted).Render(fmt.Sprintf(" %s", metadata.Name))
	if metadata.Version != "" {
		version := lipgloss.NewStyle().Foreground(fgMuted).Render(fmt.Sprintf(" v%s", metadata.Version))
		fmt.Println(nameLabel + nameValue + version)
	} else {
		fmt.Println(nameLabel + nameValue)
	}

	if metadata.Description != "" {
		descLabel := lipgloss.NewStyle().Foreground(primary).Bold(true).MarginLeft(1).Render("Description:")
		descValue := lipgloss.NewStyle().Foreground(fgMuted).Render(fmt.Sprintf(" %s", metadata.Description))
		fmt.Println(descLabel + descValue)
	}

	fmt.Println()
}
