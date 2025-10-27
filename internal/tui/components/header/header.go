package header

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"opperator/version"
	"tui/styles"
)

type Header interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (tea.Model, tea.Cmd)
	View() string

	SetWidth(width int) tea.Cmd
	SetDetailsOpen(open bool)
	ShowingDetails() bool

	SetMeta(title, model, status, hint string)
	SetUpdateAvailable(available bool)
}

type header struct {
	width       int
	detailsOpen bool

	title           string
	model           string
	status          string
	hint            string
	updateAvailable bool
}

func New() Header { return &header{} }

func (h *header) Init() tea.Cmd { return nil }

func (h *header) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return h, nil }

func (h *header) View() string {
	if h.width <= 0 {
		return ""
	}
	t := styles.CurrentTheme()

	label := "Opperator"
	versionStr := " " + version.Get()

	// Calculate update notice width if present
	updateNoticeWidth := 0
	var styledNotice string
	if h.updateAvailable {
		// Style the update notice: primary text for message, secondary for command
		updateText := lipgloss.NewStyle().
			Foreground(t.Primary).
			Bold(true).
			Render("New update, run: ")

		updateCommand := lipgloss.NewStyle().
			Foreground(t.Secondary).
			Bold(false).
			Render("op version update")

		styledNotice = updateText + updateCommand
		updateNoticeWidth = lipgloss.Width(styledNotice) + 2 // +2 for spacing
	}

	// Calculate available width for the pattern
	labelWidth := lipgloss.Width(label)
	versionWidth := lipgloss.Width(versionStr)
	availableWidth := h.width - labelWidth - versionWidth - updateNoticeWidth

	// Build left side: label + version + pattern (ending with ⁘)
	line := ""
	if availableWidth > 2 {
		// Each repeat is 2 chars ("⁘⁙"), plus 1 leading space and 1 trailing "⁘"
		repeatCount := (availableWidth - 2) / 2
		if repeatCount > 0 {
			line = " " + strings.Repeat("⁘⁙", repeatCount) + "⁘"
		}
	}

	line = styles.ApplyBoldForegroundGrad(line, t.Primary, t.BgBaseLighter)
	label = t.S().Title.Bold(true).Render(label)
	versionStr = lipgloss.NewStyle().Foreground(t.Primary).Bold(false).Render(versionStr)

	leftSide := lipgloss.JoinHorizontal(lipgloss.Top, label, versionStr, line)

	// Build the full header
	var result string
	if h.updateAvailable {
		// Use lipgloss to place left content on left and update notice on right
		rightSide := lipgloss.NewStyle().
			Align(lipgloss.Right).
			Width(h.width - lipgloss.Width(leftSide)).
			Render(styledNotice)

		result = lipgloss.JoinHorizontal(lipgloss.Top, leftSide, rightSide)
	} else {
		result = leftSide
	}

	// Ensure exact width rendering
	return t.S().Base.Width(h.width).Render(result)
}

func (h *header) SetWidth(width int) tea.Cmd { h.width = width; return nil }
func (h *header) SetDetailsOpen(open bool)   { h.detailsOpen = open }
func (h *header) ShowingDetails() bool       { return h.detailsOpen }
func (h *header) SetMeta(title, model, status, hint string) {
	h.title, h.model, h.status, h.hint = title, model, status, hint
}
func (h *header) SetUpdateAvailable(available bool) {
	h.updateAvailable = available
}
