package onboarding

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	tea2 "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
	"gopkg.in/yaml.v3"
	"opperator/internal/config"
	"opperator/internal/credentials"
	"opperator/internal/daemon"
	"tui/components/anim"
)

type OnboardingConfig struct {
	OnboardingComplete bool `yaml:"onboarding_complete"`
}

const wizardMaxWidth = 80

var (
	wizardRed       = lipgloss.AdaptiveColor{Light: "#FE5F86", Dark: "#FE5F86"}
	wizardPrimary   = lipgloss.AdaptiveColor{Light: "#f7c0af", Dark: "#f7c0af"}
	wizardBgLighter = lipgloss.Color("#3ccad7")
)

// wrapCmd converts a bubbletea v2 Cmd to v1 Cmd
func wrapCmd(cmd tea2.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		return msg
	}
}

type wizardStyles struct {
	Base,
	HeaderText,
	ErrorHeaderText,
	Help lipgloss.Style
}

func newWizardStyles(lg *lipgloss.Renderer) wizardStyles {
	s := wizardStyles{}
	s.Base = lg.NewStyle().Padding(0)
	s.HeaderText = lg.NewStyle().Foreground(wizardPrimary).Bold(true).Padding(0)
	s.ErrorHeaderText = s.HeaderText.Foreground(wizardRed)
	s.Help = lg.NewStyle().Foreground(lipgloss.Color("240"))
	return s
}

type wizardModel struct {
	width       int
	lg          *lipgloss.Renderer
	styles      wizardStyles
	form        *huh.Form
	cancelled   bool
	creditsAnim *anim.Anim
}

func newWizardModel() *wizardModel {
	lg := lipgloss.DefaultRenderer()
	styles := newWizardStyles(lg)
	width := wizardMaxWidth - styles.Base.GetHorizontalFrameSize()
	if width <= 0 {
		width = wizardMaxWidth
	}
	m := &wizardModel{
		width:  width,
		lg:     lg,
		styles: styles,
	}

	theme := createHuhTheme()
	theme.FieldSeparator = lipgloss.NewStyle().SetString("\n")
	theme.Help.FullKey = theme.Help.FullKey.MarginTop(1)

	// Create animation for the credits text
	// Detect if terminal supports TrueColor for gradients
	profile := termenv.ColorProfile()
	supportsGradients := profile == termenv.TrueColor

	// Define colors for animation (using color.RGBA for compatibility)
	primaryColor := color.RGBA{R: 0xf7, G: 0xc0, B: 0xaf, A: 0xff} // #f7c0af
	accentColor := color.RGBA{R: 0x3c, G: 0xca, B: 0xd7, A: 0xff}  // #3ccad7

	// Use gradient only if terminal supports it, otherwise use solid color
	colorB := primaryColor
	if supportsGradients {
		colorB = accentColor
	}

	m.creditsAnim = anim.New(anim.Settings{
		Label:                "$10 free credits",
		BuildLabel:           true,
		BuildInterval:        50 * time.Millisecond,
		BuildDelay:           200 * time.Millisecond,
		ShufflePrelude:       300 * time.Millisecond,
		CycleReveal:          true,
		DisplayDuration:      2 * time.Second,
		ScrambleBackDuration: 800 * time.Millisecond,
		GradColorA:           primaryColor,
		GradColorB:           colorB,
		LabelColor:           primaryColor,
		CycleColors:          supportsGradients,
		ShowEllipsis:         false,
	})

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("").
				Description("Opperator runs in the background and provides a\nterminal interface for managing your AI agents.\n\nThis setup will:\n\n 1. Connect your Opper account\n 2. Start the background process\n\nPress Enter to continue."),
		),
	).
		WithTheme(theme).
		WithWidth(80).
		WithShowHelp(false).
		WithShowErrors(false)

	return m
}

func (m *wizardModel) Init() tea.Cmd {
	return tea.Batch(m.form.Init(), wrapCmd(m.creditsAnim.Init()))
}

func (m *wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width <= 0 {
			break
		}
		width := min(msg.Width, wizardMaxWidth)
		width -= m.styles.Base.GetHorizontalFrameSize()
		if width <= 0 {
			width = wizardMaxWidth
		}
		m.width = width
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.cancelled = true
			return m, tea.Quit
		}
	}

	var cmds []tea.Cmd

	// Update animation
	animModel, animCmd := m.creditsAnim.Update(msg)
	if a, ok := animModel.(*anim.Anim); ok {
		m.creditsAnim = a
		cmds = append(cmds, wrapCmd(animCmd))
	}

	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
		cmds = append(cmds, cmd)
	}

	if m.form.State == huh.StateCompleted {
		cmds = append(cmds, tea.Quit)
		return m, tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
}

func (m *wizardModel) View() string {
	if m.form == nil {
		return ""
	}

	if m.cancelled {
		return m.styles.Base.Render(m.appBoundaryView("Opperator setup cancelled") + "\n")
	}

	switch m.form.State {
	case huh.StateCompleted:
		header := m.appBoundaryView("Opperator")
		footer := m.styles.Help.Render("Press Enter to continue")
		return m.styles.Base.Render(header + "\n\n" + footer)
	default:
		header := m.appBoundaryView("Opperator")
		formView := m.form.View()

		// Inject animation
		animView := m.creditsAnim.View()
		formView = strings.Replace(formView,
			"1. Connect your Opper account",
			"1. Connect your Opper account ("+animView+")",
			1)

		// Apply margin directly in the output without re-rendering
		lines := strings.Split(formView, "\n")
		var bodyLines []string
		for _, line := range lines {
			bodyLines = append(bodyLines, " "+line)
		}
		body := strings.Join(bodyLines, "\n")

		if errs := m.errorView(); errs != "" {
			header = m.appErrorBoundaryView(errs)
		}

		// Don't use lipgloss Render to avoid escaping animation ANSI codes
		return header + "\n" + body + "\n\n"
	}
}

func (m *wizardModel) errorView() string {
	var out strings.Builder
	for _, err := range m.form.Errors() {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(err.Error())
	}
	return out.String()
}

func (m *wizardModel) appBoundaryView(text string) string {
	if m.width <= 0 {
		return ""
	}

	// Use ASCII art header for "Opperator"
	if text == "Opperator" {
		ascii := `         ⁘⁘⁘⁘⁘⁘⁘⁘⁘
      ⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘
    ⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘
  ⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘
 ⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘
 ⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘
⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘     ⁘
⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘      ⁘⁘⁘
⁘⁘⁘⁘⁘⁘⁘⁘⁘       ⁘⁘⁘⁘⁘
⁘⁘⁘           ⁘⁘⁘⁘⁘⁘⁘⁘⁘  Opperator
⁘⁘⁘⁘⁘⁘⁘⁘⁘       ⁘⁘⁘⁘⁘
⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘      ⁘⁘⁘
⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘     ⁘
 ⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘
  ⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘
   ⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘
     ⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘
       ⁘⁘⁘⁘⁘⁘⁘⁘⁘⁘
          ⁘⁘⁘⁘⁘⁘⁘⁘`
		return m.styles.HeaderText.MarginLeft(2).MarginTop(2).Render(ascii)
	}

	// Style the label
	styledText := m.styles.HeaderText.Render(text)

	// Build pattern line with gradient fade (ending with ⁘)
	line := " " + strings.Repeat("⁘⁙", 30) + "⁘"
	styledLine := applyGradient(line, wizardPrimary, wizardBgLighter)

	// Join label and pattern
	result := lipgloss.JoinHorizontal(lipgloss.Top, styledText, styledLine)

	// Ensure it renders at the proper width
	return lipgloss.NewStyle().Width(m.width).Render(result)
}

func (m *wizardModel) appErrorBoundaryView(text string) string {
	if m.width <= 0 {
		return ""
	}

	// Style the label with error styling
	styledText := m.styles.ErrorHeaderText.Render(text)

	// Build pattern line with error styling
	line := " " + strings.Repeat("/", 80)
	styledLine := lipgloss.NewStyle().Foreground(wizardRed).Render(line)

	// Join label and pattern
	result := lipgloss.JoinHorizontal(lipgloss.Top, styledText, styledLine)

	// Ensure it renders at the proper width
	return lipgloss.NewStyle().Width(m.width).Render(result)
}

func min(x, y int) int {
	if x > y {
		return y
	}
	return x
}

// applyGradient applies a gradient from one color to another across the text.
// Falls back to solid color if the terminal doesn't support TrueColor.
func applyGradient(text string, from, to color.Color) string {
	rs := []rune(text)
	n := len(rs)
	if n == 0 {
		return ""
	}

	// Check if terminal supports TrueColor
	profile := termenv.ColorProfile()
	if profile != termenv.TrueColor {
		// Fallback to solid color using 'from' color
		c1, _ := colorful.MakeColor(from)
		hex := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", uint8(c1.R*255), uint8(c1.G*255), uint8(c1.B*255)))
		return lipgloss.NewStyle().Foreground(hex).Bold(true).Render(text)
	}

	// Apply gradient for TrueColor terminals
	c1, _ := colorful.MakeColor(from)
	c2, _ := colorful.MakeColor(to)
	var out string
	for i, r := range rs {
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		c := c1.BlendLab(c2, t)
		hex := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", uint8(c.R*255), uint8(c.G*255), uint8(c.B*255)))
		out += lipgloss.NewStyle().Foreground(hex).Bold(true).Render(string(r))
	}
	return out
}

func RunWizard() error {
	model := newWizardModel()
	program := tea.NewProgram(model)

	finalModel, teaErr := program.Run()
	if teaErr != nil {
		return teaErr
	}

	wm, ok := finalModel.(*wizardModel)
	if !ok {
		return fmt.Errorf("unexpected wizard model type %T", finalModel)
	}
	if wm.cancelled {
		return errors.New("cancelled")
	}

	cfg := OnboardingConfig{
		OnboardingComplete: true,
	}

	if err := ensureOpperAPIKey(); err != nil {
		if err.Error() == "cancelled" {
			return err
		}
		return fmt.Errorf("connect to opper: %w", err)
	}

	if err := savePreferences(cfg); err != nil {
		return fmt.Errorf("failed to save preferences: %w", err)
	}

	// Show completion message using same styling as intro
	fg := lipgloss.Color("#dddddd")
	baseStyle := lipgloss.NewStyle().Foreground(fg)
	highlightStyle := lipgloss.NewStyle().Foreground(wizardPrimary).Bold(true)

	fmt.Println()
	fmt.Println(baseStyle.Render(" ✔︎ Authentication successful!"))
	fmt.Println()
	fmt.Print(baseStyle.Render(" Run '"))
	fmt.Print(highlightStyle.Render("op"))
	fmt.Print(baseStyle.Render("' or '"))
	fmt.Print(highlightStyle.Render("opperator"))
	fmt.Print(baseStyle.Render("' to launch."))
	fmt.Println()
	fmt.Println()

	return nil
}

const (
	opperAuthorizeHost = "platform.opper.ai"
	opperAuthorizePath = "/authorize/opperator"
	opperCallbackAddr  = "127.0.0.1:3333"
	opperCallbackURL   = "http://localhost:3333/"
)

func ensureOpperAPIKey() error {
	hasKey, err := credentials.HasAPIKey()
	if err != nil {
		return fmt.Errorf("check existing Opper API key: %w", err)
	}
	if hasKey {
		fg := lipgloss.Color("#dddddd")
		baseStyle := lipgloss.NewStyle().Foreground(fg)
		message := baseStyle.Render("\nOpper API key already configured.\n")
		fmt.Print(message)
		return nil
	}

	authURL := buildOpperAuthorizeURL()

	theme := createHuhTheme()
	theme.FieldSeparator = lipgloss.NewStyle().SetString("")

	// Create styled description with highlighted "Enter"
	fg := lipgloss.Color("#dddddd")
	primary := wizardPrimary
	highlightStyle := lipgloss.NewStyle().Foreground(primary).Bold(true)

	description := " Press " + highlightStyle.Render("Enter") + lipgloss.NewStyle().Foreground(fg).Render(" to open your browser and authorize\n Opperator with your Opper account.")

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("").
				Description(description),
		),
	).
		WithTheme(theme).
		WithWidth(80).
		WithShowHelp(false).
		WithShowErrors(false)

	if err := form.Run(); err != nil {
		return errors.New("cancelled")
	}

	spinnerStyle := lipgloss.NewStyle().MarginLeft(2).Foreground(lipgloss.Color("#f7c0af"))
	var key string
	var captureErr error

	err = spinner.New().
		Title("Waiting for authorization...").
		Style(spinnerStyle).
		Action(func() {
			key, captureErr = captureOpperAPIKey(authURL, 5*time.Minute)
		}).
		Run()

	if err != nil {
		return errors.New("cancelled")
	}
	if captureErr != nil {
		return captureErr
	}

	if err := credentials.SetAPIKey(key); err != nil {
		return fmt.Errorf("store Opper API key: %w", err)
	}

	if err := credentials.RegisterSecret(credentials.OpperAPIKeyName); err != nil {
		return fmt.Errorf("register Opper API key: %w", err)
	}

	return nil
}

func buildOpperAuthorizeURL() string {
	u := url.URL{
		Scheme: "https",
		Host:   opperAuthorizeHost,
		Path:   opperAuthorizePath,
	}
	q := u.Query()
	q.Set("callback", opperCallbackURL)
	u.RawQuery = q.Encode()
	return u.String()
}

func captureOpperAPIKey(authURL string, timeout time.Duration) (string, error) {
	listener, err := net.Listen("tcp", opperCallbackAddr)
	if err != nil {
		return "", fmt.Errorf("start Opper callback listener on %s: %w", opperCallbackAddr, err)
	}
	defer listener.Close()

	keyCh := make(chan string, 1)
	errCh := make(chan error, 1)

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if errParam := strings.TrimSpace(query.Get("error")); errParam != "" {
				message := errParam
				if desc := strings.TrimSpace(query.Get("error_description")); desc != "" {
					message = fmt.Sprintf("%s: %s", errParam, desc)
				}
				http.Error(w, "Authorization failed. You can close this tab.", http.StatusBadRequest)
				select {
				case errCh <- fmt.Errorf("opper authorization failed: %s", message):
				default:
				}
				return
			}

			key := strings.TrimSpace(query.Get("key"))
			if key == "" {
				http.Error(w, "Missing key parameter.", http.StatusBadRequest)
				return
			}

			fmt.Fprintln(w, "Opper authorization complete. You can close this tab.")
			select {
			case keyCh <- key:
			default:
			}
		}),
	}

	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case errCh <- fmt.Errorf("callback server error: %w", err):
			default:
			}
		}
	}()

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	// Open browser once
	if err := openBrowser(authURL); err != nil {
		fmt.Printf("  • Unable to open browser automatically (%v).\n", err)
		fmt.Printf("    Open this URL manually: %s\n", authURL)
	}

	select {
	case key := <-keyCh:
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			return "", fmt.Errorf("received empty Opper API key")
		}
		return trimmed, nil
	case err := <-errCh:
		return "", err
	case <-time.After(timeout):
		return "", fmt.Errorf("timed out waiting for Opper authorization callback")
	}
}

func openBrowser(targetURL string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", targetURL).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL).Start()
	default:
		return exec.Command("xdg-open", targetURL).Start()
	}
}

func savePreferences(cfg OnboardingConfig) error {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return err
	}

	prefsFile := filepath.Join(configDir, "preferences.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(prefsFile, data, 0644)
}

func StartServices() error {
	return startDaemonService()
}

func startDaemonService() error {
	// CRITICAL: Do not spawn if already running
	if daemon.IsRunning() {
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Start daemon in background
	fg := lipgloss.Color("#dddddd")
	baseStyle := lipgloss.NewStyle().Foreground(fg)
	fmt.Print(baseStyle.Render("Starting daemon... "))
	daemonCmd := exec.Command(executable, "daemon", "start")
	if err := daemonCmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	fmt.Print(baseStyle.Render("done.\n"))

	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		if daemon.IsRunning() {
			break
		}
		if i == 9 {
			return fmt.Errorf("daemon failed to start properly")
		}
	}

	return nil
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
