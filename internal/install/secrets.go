package install

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"opperator/internal/credentials"
)

// runSecretConfigWizard prompts the user to configure required secrets
// Returns:
//   - secretMappings: map of env var name -> secret name to use
//   - secretsToCreate: map of secret name -> value for new secrets
//   - secretRenames: map of old secret name -> new secret name (for file renaming)
func runSecretConfigWizard(requiredSecrets []string, agentName string) (map[string]string, map[string]string, map[string]string, error) {
	if len(requiredSecrets) == 0 {
		return map[string]string{}, map[string]string{}, map[string]string{}, nil
	}

	// First, ask if user wants to configure now or later
	var configureNow bool
	theme := createHuhTheme()

	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("This agent requires %d secret(s). Configure them now?", len(requiredSecrets))).
				Description("You can also configure them later with 'op secret set'").
				Value(&configureNow).
				Affirmative("Configure now").
				Negative("Skip"),
		),
	).
		WithTheme(theme).
		WithShowHelp(false).
		WithShowErrors(false)

	if err := confirmForm.Run(); err != nil {
		return nil, nil, nil, err
	}

	if !configureNow {
		return map[string]string{}, map[string]string{}, map[string]string{}, nil
	}

	// Get list of existing secrets
	existingSecrets, err := credentials.ListSecrets()
	if err != nil {
		// Non-fatal: just proceed without checking existing secrets
		existingSecrets = []string{}
	}

	// Create a map for quick lookup
	existingSecretsMap := make(map[string]bool)
	for _, s := range existingSecrets {
		existingSecretsMap[s] = true
	}

	// For each required secret, determine the configuration approach
	secretMappings := make(map[string]string)  // Maps env var -> secret name
	secretsToCreate := make(map[string]string) // Maps secret name -> value
	secretRenames := make(map[string]string)   // Maps old secret name -> new secret name (for file renaming)

	for _, secretName := range requiredSecrets {
		secretExists := existingSecretsMap[secretName]

		if secretExists {
			// Secret exists - offer three options
			var choice string
			newSecretName := fmt.Sprintf("%s_%s", secretName, strings.ToUpper(strings.ReplaceAll(agentName, "-", "_")))

			form := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title(fmt.Sprintf("Secret '%s' already exists. How should we handle it?", secretName)).
						Options(
							huh.NewOption(fmt.Sprintf("Use existing secret '%s'", secretName), "use_existing"),
							huh.NewOption(fmt.Sprintf("Create new secret '%s'", newSecretName), "create_new"),
							huh.NewOption("Skip (configure later)", "skip"),
						).
						Value(&choice),
				),
			).
				WithTheme(theme).
				WithShowHelp(false).
				WithShowErrors(false)

			if err := form.Run(); err != nil {
				return nil, nil, nil, err
			}

			switch choice {
			case "use_existing":
				secretMappings[secretName] = secretName
			case "create_new":
				// Prompt for the value of the new secret
				var newValue string
				valueForm := huh.NewForm(
					huh.NewGroup(
						huh.NewInput().
							Title(fmt.Sprintf("Enter value for new secret '%s'", newSecretName)).
							Value(&newValue).
							Password(true).
							Validate(func(s string) error {
								if strings.TrimSpace(s) == "" {
									return errors.New("value cannot be empty")
								}
								return nil
							}),
					),
				).
					WithTheme(theme).
					WithShowHelp(false).
					WithShowErrors(false)

				if err := valueForm.Run(); err != nil {
					return nil, nil, nil, err
				}

				secretMappings[secretName] = newSecretName
				secretsToCreate[newSecretName] = newValue
				secretRenames[secretName] = newSecretName // Track rename for file updates
			case "skip":
				// Don't add to mappings - will be configured later
			}
		} else {
			// Secret doesn't exist - just prompt for value
			var newValue string
			var skipSecret bool

			valueForm := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title(fmt.Sprintf("Enter value for '%s'", secretName)).
						Description("(leave blank to skip)").
						Value(&newValue).
						Password(true),
				),
			).
				WithTheme(theme).
				WithShowHelp(false).
				WithShowErrors(false)

			if err := valueForm.Run(); err != nil {
				return nil, nil, nil, err
			}

			if strings.TrimSpace(newValue) != "" {
				secretMappings[secretName] = secretName
				secretsToCreate[secretName] = newValue
			} else {
				skipSecret = true
			}

			_ = skipSecret // Mark as used
		}
	}

	return secretMappings, secretsToCreate, secretRenames, nil
}

// createHuhTheme creates a consistent theme for forms
// This is reused from the publish package to maintain consistency
func createHuhTheme() *huh.Theme {
	primary := lipgloss.Color("#f7c0af")         // orangish/peach
	primaryMuted := lipgloss.Color("#A37E73")
	fgMuted := lipgloss.Color("#b3b3b3")         // muted gray
	white := lipgloss.Color("#ffffff")

	theme := huh.ThemeBase16()

	// Light touch: inherit Base16, just tint the primary accents (matching publish wizard)
	theme.Focused.NoteTitle = theme.Focused.NoteTitle.Foreground(primary).Bold(true).MarginLeft(1).Faint(false)
	theme.Blurred.NoteTitle = theme.Focused.NoteTitle.Foreground(primary).Bold(false).MarginLeft(1).Faint(false)
	theme.Focused.Description = theme.Focused.Description.MarginLeft(1).Foreground(fgMuted)
	theme.Focused.Base = theme.Focused.Base.MarginBottom(1).BorderForeground(primary)
	theme.Blurred.Base = theme.Focused.Base.MarginBottom(1).BorderForeground(fgMuted)
	theme.Focused.Title = theme.Focused.Title.Foreground(primary).Bold(false)
	theme.Blurred.Title = theme.Blurred.Title.Foreground(primary).Bold(false).Faint(true)
	theme.Focused.Description = theme.Focused.Title.Foreground(fgMuted).Bold(false)
	theme.Blurred.Description = theme.Blurred.Description.Foreground(fgMuted).Bold(false).Faint(true)
	theme.Focused.NoteTitle = theme.Focused.NoteTitle.Foreground(primary).Bold(false)
	theme.Blurred.NoteTitle = theme.Blurred.NoteTitle.Foreground(primary).Bold(false).Faint(true)
	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(primary).Bold(true)
	theme.Blurred.SelectSelector = theme.Blurred.SelectSelector.Foreground(primary).Bold(true).Faint(true)
	theme.Focused.MultiSelectSelector = theme.Focused.MultiSelectSelector.Foreground(primary).Bold(true)
	theme.Blurred.MultiSelectSelector = theme.Blurred.MultiSelectSelector.Foreground(primary).Bold(true).Faint(true)
	theme.Focused.Option = theme.Focused.Option.Foreground(white)
	theme.Blurred.Option = theme.Blurred.Option.Foreground(white).Faint(true)
	theme.Focused.SelectedOption = theme.Focused.SelectedOption.Foreground(primary).Bold(true)
	theme.Blurred.SelectedOption = theme.Blurred.SelectedOption.Foreground(primary).Bold(true).Faint(true)
	theme.Focused.SelectedPrefix = theme.Focused.SelectedPrefix.Foreground(primary)
	theme.Blurred.SelectedPrefix = theme.Blurred.SelectedPrefix.Foreground(primary).Faint(true)
	theme.Focused.FocusedButton = theme.Focused.FocusedButton.Background(primary).Bold(true)
	theme.Blurred.FocusedButton = theme.Blurred.FocusedButton.Background(primaryMuted).Bold(true).Faint(true)
	theme.Focused.TextInput.Cursor = theme.Focused.TextInput.Cursor.Foreground(primary)
	theme.Blurred.TextInput.Cursor = theme.Blurred.TextInput.Cursor.Foreground(primary).Faint(true)
	theme.Focused.TextInput.Prompt = theme.Focused.TextInput.Prompt.Foreground(primary)
	theme.Blurred.TextInput.Prompt = theme.Blurred.TextInput.Prompt.Foreground(primary).Faint(true)
	theme.Focused.TextInput.Text = theme.Focused.TextInput.Prompt.Foreground(white)
	theme.Blurred.TextInput.Text = theme.Blurred.TextInput.Text.Foreground(white).Faint(true)

	// Help styles
	theme.Help.Ellipsis = theme.Help.Ellipsis.Foreground(fgMuted)
	theme.Help.ShortKey = theme.Help.ShortKey.Foreground(fgMuted)
	theme.Help.ShortDesc = theme.Help.ShortDesc.Foreground(fgMuted)
	theme.Help.ShortSeparator = theme.Help.ShortSeparator.Foreground(fgMuted)
	theme.Help.FullKey = theme.Help.FullKey.Foreground(fgMuted)
	theme.Help.FullDesc = theme.Help.FullDesc.Foreground(fgMuted)
	theme.Help.FullSeparator = theme.Help.FullSeparator.Foreground(fgMuted)

	return theme
}

// printSecretsInfo displays information about required secrets
func printSecretsInfo(requiredSecrets []string) {
	if len(requiredSecrets) == 0 {
		return
	}

	primary := lipgloss.Color("#f7c0af")
	fgMuted := lipgloss.Color("#b3b3b3")

	header := lipgloss.NewStyle().Foreground(primary).Bold(true).MarginLeft(1).Render("Required secrets:")
	fmt.Println(header)

	for _, secret := range requiredSecrets {
		bullet := lipgloss.NewStyle().Foreground(primary).MarginLeft(1).Render("  •")
		secretName := lipgloss.NewStyle().Foreground(fgMuted).Render(" " + secret)
		fmt.Println(bullet + secretName)
	}

	fmt.Println()
}

// validateSecretName checks if a secret name is valid
func validateSecretName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("secret name cannot be empty")
	}
	return nil
}
