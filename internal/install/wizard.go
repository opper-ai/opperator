package install

import (
	"errors"
	"regexp"
	"strings"

	"github.com/charmbracelet/huh"
)

// wizardInput holds the user's input from the install wizard
type wizardInput struct {
	AgentName string
	AutoStart bool
}

// runInstallWizard runs the interactive wizard for agent installation
func runInstallWizard(metadata *AgentMetadata, defaultName string) (*wizardInput, error) {
	theme := createHuhTheme()

	input := &wizardInput{
		AgentName: defaultName,
		AutoStart: true,
	}

	fields := []huh.Field{
		huh.NewInput().
			Title("Agent name").
			Description("Local name for this agent").
			Value(&input.AgentName).
			Validate(validateAgentName),

		huh.NewConfirm().
			Title("Auto-start after install?").
			Description("Start the agent immediately after installation").
			Value(&input.AutoStart).
			Affirmative("Start").
			Negative("Skip"),
	}

	form := huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(theme).
		WithHeight(40).
		WithShowHelp(false).
		WithShowErrors(false)

	if err := form.Run(); err != nil {
		return nil, err
	}

	return input, nil
}

// validateAgentName validates an agent name
func validateAgentName(name string) error {
	name = strings.TrimSpace(name)

	if name == "" {
		return errors.New("agent name cannot be empty")
	}

	// Agent names should be alphanumeric with hyphens and underscores
	validName := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	if !validName.MatchString(name) {
		return errors.New("agent name must contain only letters, numbers, hyphens, and underscores")
	}

	return nil
}
