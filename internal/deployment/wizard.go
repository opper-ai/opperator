package deployment

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

const wizardMaxWidth = 80

// DeploymentInput holds the user's deployment configuration
type DeploymentInput struct {
	Name       string
	Provider   string // "hetzner" for now
	APIKey     string
	ServerType string // cx23, cx33, etc.
	Location   string // nbg1, fsn1, etc.
	Confirmed  bool   // Whether user confirmed deployment
}

func buildServerTypeGroup(input *DeploymentInput, serverTypes []ServerTypeOption) *huh.Group {
	options := make([]huh.Option[string], 0, len(serverTypes))
	for _, st := range serverTypes {
		// Parse price and format with 2 decimal places
		price, _ := strconv.ParseFloat(st.PriceMonthly, 64)
		label := fmt.Sprintf("%s - %d vCPU, %.0f GB RAM, %d GB SSD (~€%.2f/mo)",
			strings.ToUpper(st.Name), st.Cores, st.Memory, st.Disk, price)
		options = append(options, huh.NewOption(label, st.Name))
	}

	return huh.NewGroup(
		huh.NewSelect[string]().
			Title("Server Type").
			Description("Choose your VPS size").
			Options(options...).
			Value(&input.ServerType),
	)
}

func buildLocationGroup(input *DeploymentInput, locations []LocationOption, serverTypes []ServerTypeOption) *huh.Group {
	// Find the selected server type to get its available locations
	var availableLocationNames []string
	for _, st := range serverTypes {
		if st.Name == input.ServerType {
			availableLocationNames = st.AvailableLocations
			break
		}
	}

	// Filter locations to only those available for the selected server type
	options := make([]huh.Option[string], 0)
	for _, loc := range locations {
		// Check if this location is available for the selected server type
		isAvailable := false
		for _, availableName := range availableLocationNames {
			if loc.Name == availableName {
				isAvailable = true
				break
			}
		}

		if isAvailable {
			label := fmt.Sprintf("%s (%s)", loc.Description, loc.Name)
			options = append(options, huh.NewOption(label, loc.Name))
		}
	}

	return huh.NewGroup(
		huh.NewSelect[string]().
			Title("Location").
			Description("Choose datacenter location (filtered by server type availability)").
			Options(options...).
			Value(&input.Location),
	)
}

// RunWizardPartOne runs the first part: name and API key
func RunWizardPartOne(hasExistingKey bool) (*DeploymentInput, error) {
	input := &DeploymentInput{}

	theme := createHuhTheme()
	theme.FieldSeparator = lipgloss.NewStyle().SetString("\n")

	// Set defaults
	if input.Provider == "" {
		input.Provider = "hetzner"
	}

	// Build API key description based on whether we have an existing key
	apiKeyDesc := "Enter your Hetzner Cloud API key"
	if hasExistingKey {
		apiKeyDesc = "Enter new Hetzner API key (or leave empty to use saved key)"
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Deploy Daemon to VPS").
				Description("This wizard will deploy an Opperator daemon to a Hetzner VPS.\n\nYou'll need:\n • Hetzner Cloud API key\n • A name for this daemon\n\nPress Enter to continue."),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Daemon name").
				Description("Choose a name for this daemon (e.g., 'production', 'hetzner')").
				Value(&input.Name).
				Validate(func(s string) error {
					if s == "" {
						return errors.New("name cannot be empty")
					}
					if strings.Contains(s, " ") {
						return errors.New("name cannot contain spaces")
					}
					return nil
				}),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Hetzner API Key").
				Description(apiKeyDesc).
				Value(&input.APIKey).
				Password(true).
				Validate(func(s string) error {
					// Allow empty if we have existing key
					if s == "" && hasExistingKey {
						return nil
					}
					if s == "" {
						return errors.New("API key is required")
					}
					if len(s) < 20 {
						return errors.New("API key seems too short")
					}
					return nil
				}),
		),
	).
		WithTheme(theme).
		WithWidth(80).
		WithShowHelp(false).
		WithShowErrors(false)

	if err := form.Run(); err != nil {
		return nil, errors.New("cancelled")
	}

	return input, nil
}

// RunWizardPartTwo runs the second part: server type, location, and confirmation
func RunWizardPartTwo(input *DeploymentInput, serverTypes []ServerTypeOption, locations []LocationOption) (*DeploymentInput, error) {
	theme := createHuhTheme()
	theme.FieldSeparator = lipgloss.NewStyle().SetString("\n")

	// Step 1: Select server type
	serverTypeForm := huh.NewForm(
		buildServerTypeGroup(input, serverTypes),
	).
		WithTheme(theme).
		WithWidth(80).
		WithShowHelp(false).
		WithShowErrors(false)

	if err := serverTypeForm.Run(); err != nil {
		return nil, errors.New("cancelled")
	}

	// Step 2: Select location (now we have server type selected)
	locationForm := huh.NewForm(
		buildLocationGroup(input, locations, serverTypes),
	).
		WithTheme(theme).
		WithWidth(80).
		WithShowHelp(false).
		WithShowErrors(false)

	if err := locationForm.Run(); err != nil {
		return nil, errors.New("cancelled")
	}

	// Step 3: Confirm deployment
	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Ready to Deploy").
				Description(fmt.Sprintf("Deploy daemon '%s' to Hetzner %s in %s?", input.Name, input.ServerType, input.Location)).
				Value(&input.Confirmed).
				Affirmative("Deploy").
				Negative("Cancel"),
		),
	).
		WithTheme(theme).
		WithWidth(80).
		WithShowHelp(false).
		WithShowErrors(false)

	if err := confirmForm.Run(); err != nil {
		return nil, errors.New("cancelled")
	}

	// Check if user confirmed deployment
	if !input.Confirmed {
		return nil, errors.New("cancelled")
	}

	return input, nil
}

func createHuhTheme() *huh.Theme {
	// Define our color palette (matching the TUI theme)
	primary := lipgloss.Color("#f7c0af")  // orangish/peach
	fg := lipgloss.Color("#dddddd")       // light gray
	fgMuted := lipgloss.Color("#7f7f7f")  // muted gray
	fgSubtle := lipgloss.Color("#888888") // subtle gray
	bg := lipgloss.Color("#101012")       // dark bg
	error := lipgloss.Color("#bf5d47")    // red
	success := lipgloss.Color("#87bf47")  // green

	theme := huh.ThemeBase16()

	base := lipgloss.NewStyle().Foreground(fg)

	// Focused field styles
	theme.Focused.Base = base.MarginLeft(1)
	theme.Focused.Title = base.Foreground(primary).Bold(true)
	theme.Focused.Description = base.Foreground(fg)
	theme.Focused.ErrorIndicator = base.Foreground(error)
	theme.Focused.ErrorMessage = base.Foreground(error)

	theme.Form = base.Padding(0)

	// Select/MultiSelect styles
	theme.Focused.SelectSelector = base.Foreground(primary).Bold(true)
	theme.Focused.MultiSelectSelector = base.Foreground(primary).Bold(true)
	theme.Focused.SelectedOption = base.Foreground(primary).Bold(true)
	theme.Focused.SelectedPrefix = base.Foreground(success).Bold(true).SetString("✓ ")
	theme.Focused.UnselectedOption = base
	theme.Blurred.UnselectedOption = base.Foreground(error)
	theme.Focused.UnselectedPrefix = base.Foreground(fgMuted).SetString("> ")
	theme.Focused.Option = base

	// Button styles
	theme.Focused.FocusedButton = base.Background(primary).Foreground(bg).Bold(true).Padding(0, 2)
	theme.Focused.BlurredButton = base.Foreground(fgMuted).Padding(0).MarginLeft(1)

	// Note/Card styles
	theme.Focused.NoteTitle = base.Foreground(primary).Bold(true)
	theme.Focused.Card = base.Padding(0)

	// Text input styles
	theme.Focused.TextInput.Cursor = base.Foreground(primary)
	theme.Focused.TextInput.Placeholder = base.Foreground(fgSubtle)
	theme.Focused.TextInput.Prompt = base.Foreground(primary)

	// Blurred field styles
	theme.Blurred.Base = base
	theme.Blurred.Title = base.Foreground(fgMuted)
	theme.Blurred.Description = base.Foreground(fg)
	theme.Blurred.NoteTitle = base.Foreground(fgMuted)
	theme.Blurred.TextInput.Placeholder = base.Foreground(fgSubtle)
	theme.Blurred.TextInput.Prompt = base.Foreground(fgMuted)

	// Form-wide styles
	theme.Form = base
	theme.Group = base.Background(lipgloss.Color("#151517")).Padding(0).MarginBottom(0)

	return theme
}
