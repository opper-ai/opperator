package publish

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
)

const maxWidth = 80

// runStepWithSpinner runs a step with a consistent spinner style matching the form theme.
// Shows a checkmark after completion or red X on failure to preserve progress visibility.
func runStepWithSpinner(title string, action func() error) error {
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

func printError(err error) {
	errorRed := lipgloss.Color("#bf5d47")
	fgMuted := lipgloss.Color("#b3b3b3")

	errorLabel := lipgloss.NewStyle().Foreground(errorRed).Bold(true).MarginLeft(1).Render("Error:")
	message := lipgloss.NewStyle().Foreground(fgMuted).Render(" " + err.Error())
	fmt.Println("\n" + errorLabel + message)
}

func printWarning(err error) {
	warningYellow := lipgloss.Color("#ffc107")
	fgMuted := lipgloss.Color("#b3b3b3")

	warningLabel := lipgloss.NewStyle().Foreground(warningYellow).Bold(true).MarginLeft(1).Render("Warning:")
	message := lipgloss.NewStyle().Foreground(fgMuted).Render(" " + err.Error())
	fmt.Println("\n" + warningLabel + message + "\n")
}

func printInfo(msg string) {
	warningYellow := lipgloss.Color("#ffc107")

	icon := lipgloss.NewStyle().Foreground(warningYellow).MarginLeft(1).Render("⚠")
	message := lipgloss.NewStyle().Foreground(warningYellow).Render(" " + msg)
	fmt.Print(icon + message + "\n")
}

func printSuccessMessage(state githubState, packageName, repoName, tempDir string, scanFindings int) {
	primary := lipgloss.Color("#f7c0af")
	fgMuted := lipgloss.Color("#b3b3b3")

	// Build success message based on whether GitHub CLI was used
	if state.CLIInstalled && state.Connected {
		// Full success - repo was created and pushed
		repoURL := fmt.Sprintf("https://github.com/%s", packageName)
		repoNameStyled := lipgloss.NewStyle().Foreground(primary).Bold(true).Render(packageName)
		messageText := lipgloss.NewStyle().Foreground(fgMuted).Render("Published ")
		successText := lipgloss.NewStyle().Foreground(fgMuted).Render(" successfully!")
		link := lipgloss.NewStyle().Foreground(fgMuted).Faint(true).Render(fmt.Sprintf("%s", repoURL))

		fmt.Printf("\n%s\n%s\n",
			lipgloss.NewStyle().MarginLeft(1).Render(messageText+repoNameStyled+successText),
			lipgloss.NewStyle().MarginLeft(1).Render(link))
	} else {
		// Partial success - repo initialized locally, manual steps needed
		secondary := lipgloss.Color("#3ccad7") // cyan
		repoPath := tempDir
		successMsg := lipgloss.NewStyle().Foreground(primary).Bold(true).MarginLeft(1).Render("Repository initialized locally")
		fmt.Printf("\n%s\n", successMsg)

		fmt.Println()
		nextStepsLabel := lipgloss.NewStyle().Foreground(primary).Bold(true).MarginLeft(1).Render("Next steps:")
		fmt.Println(nextStepsLabel)

		// Step 1: Create repo on GitHub
		step1Num := lipgloss.NewStyle().Foreground(primary).MarginLeft(1).Render("  1.")
		step1Text := lipgloss.NewStyle().Foreground(fgMuted).Render(" Create a new GitHub repository named ")
		repoNameStyled := lipgloss.NewStyle().Foreground(secondary).Bold(true).Render(repoName)
		fmt.Println(step1Num + step1Text + repoNameStyled)
		step1Link := lipgloss.NewStyle().Foreground(fgMuted).Faint(true).MarginLeft(1).Render("     https://github.com/new")
		fmt.Println(step1Link)

		// Step 2: Navigate to directory
		step2Num := lipgloss.NewStyle().Foreground(primary).MarginLeft(1).Render("  2.")
		step2Text := lipgloss.NewStyle().Foreground(fgMuted).Render(" Navigate to the repository:")
		fmt.Println("\n" + step2Num + step2Text)
		step2Cmd := lipgloss.NewStyle().Foreground(secondary).MarginLeft(1).Render(fmt.Sprintf("     cd %s", repoPath))
		fmt.Println(step2Cmd)

		// Step 3: Push to GitHub
		step3Num := lipgloss.NewStyle().Foreground(primary).MarginLeft(1).Render("  3.")
		step3Text := lipgloss.NewStyle().Foreground(fgMuted).Render(" Push to GitHub:")
		fmt.Println("\n" + step3Num + step3Text)
		step3Cmd := lipgloss.NewStyle().Foreground(secondary).MarginLeft(1).Render("     git push -u origin main")
		fmt.Println(step3Cmd)
	}

	if scanFindings > 0 {
		fmt.Println("\nNote: repo has acknowledged findings. Consider refactoring with the Builder agent.")
	}
}
