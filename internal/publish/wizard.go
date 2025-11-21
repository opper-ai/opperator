package publish

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

type wizardInput struct {
	GitHubUser   string
	RepoName     string
	Visibility   string
	RunScan      bool
	Confirmed    bool
	AccountState githubState
}

func runWizard(agentName string, input *wizardInput) error {
	theme := createHuhTheme()
	theme.FieldSeparator = lipgloss.NewStyle().SetString("\n")

	// Build form fields
	var fields []huh.Field

	// Add GitHub username field if not detected
	if input.GitHubUser == "" {
		fields = append(fields,
			huh.NewInput().
				Key("user").
				Title("GitHub username").
				Description("Your GitHub username (e.g., octocat)").
				Value(&input.GitHubUser).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("GitHub username required")
					}
					return nil
				}),
		)
	}

	fields = append(fields,
		huh.NewInput().
			Key("repo").
			Title("Repository name").
			Description("Repo will be created as <your-user>/<name>").
			Value(&input.RepoName).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("repo name required")
				}
				return nil
			}),

		huh.NewSelect[string]().
			Key("visibility").
			Title("Visibility").
			Description("Configure who can see the repository on GitHub").
			Options(
				huh.NewOption("Public", "public"),
				huh.NewOption("Private", "private"),
			).
			Value(&input.Visibility),

		huh.NewConfirm().
			Key("scan").
			Title("Run code security scan?").
			Description("Optional safety scan of files for exposed secrets before publishing.").
			Value(&input.RunScan).
			Affirmative("Run").
			Negative("Skip"),

		huh.NewConfirm().
			Key("confirm").
			Title("Ready to publish?").
			Validate(func(v bool) error {
				if !v {
					return fmt.Errorf("Cancel to exit")
				}
				return nil
			}).
			Value(&input.Confirmed).
			Affirmative("Publish").
			Negative("Cancel"),
	)

	form := huh.NewForm(
		huh.NewGroup(fields...),
	).
		WithTheme(theme).
		WithHeight(40).
		WithShowHelp(false).
		WithShowErrors(false)

	if err := form.Run(); err != nil {
		return err
	}

	// Use detected user if available, otherwise use what user entered
	if input.AccountState.DetectedUser != "" {
		input.GitHubUser = strings.TrimSpace(input.AccountState.DetectedUser)
	} else {
		input.GitHubUser = strings.TrimSpace(input.GitHubUser)
	}

	if !input.Confirmed {
		return errors.New("cancelled")
	}
	return nil
}

func createHuhTheme() *huh.Theme {
	primary := lipgloss.Color("#f7c0af") // orangish/peach
	primaryMuted := lipgloss.Color("#A37E73")
	fgMuted := lipgloss.Color("#b3b3b3") // muted gray
	white := lipgloss.Color("#ffffff")

	theme := huh.ThemeBase16()

	// Light touch: inherit Base16, just tint the primary accents.
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

	return theme
}

func confirmProceedAfterFindings() (bool, error) {
	confirm := false
	theme := createHuhTheme()

	fmt.Println() // Add margin top

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Proceed despite findings?").
				Description("Proceeding will publish the repo as-is. Otherwise, refactor with the Builder agent and rerun publish.").
				Value(&confirm).
				Affirmative("Proceed").
				Negative("Cancel"),
		),
	).WithTheme(theme).WithShowErrors(false).WithShowHelp(false)
	if err := form.Run(); err != nil {
		return false, err
	}

	return confirm, nil
}

func confirmProceedAfterScanError(scanErr error) (bool, error) {
	confirm := false
	theme := createHuhTheme()

	fmt.Println() // Add margin top

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Scan failed — continue without results?").
				Description(fmt.Sprintf("Gemini scan did not complete (%v).\nProceed to publish anyway?", scanErr)).
				Value(&confirm).
				Affirmative("Proceed").
				Negative("Cancel"),
		),
	).WithTheme(theme).WithShowErrors(false).WithShowHelp(false)
	if err := form.Run(); err != nil {
		return false, err
	}

	// Move cursor up to absorb the extra newline from the form
	fmt.Print("\033[1A")

	return confirm, nil
}
