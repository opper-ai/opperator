package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tui/styles"
)

func (m *Model) View() string {
	if m == nil || m.w == 0 || m.h == 0 {
		return ""
	}

	if m.toolDetail != nil {
		return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, m.toolDetail.View())
	}

	if ui := m.secretPromptUI(); ui != nil && ui.active() {
		return m.renderViewWithSecretOverlay()
	}

	if ui := m.permissionUI(); ui != nil && ui.active() {
		return m.renderViewWithOverlay()
	}

	if m.convModal != nil {
		return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, m.convModal.View())
	}

	return m.renderBaseView()
}

func (m *Model) renderViewWithOverlay() string {
	base := m.renderBaseView()
	ui := m.permissionUI()
	if ui == nil || !ui.active() {
		return base
	}

	xOffset := 0
	if m.input != nil {
		xOffset = m.input.CommandPickerXOffset()
	}

	return ui.renderWithBase(base, m.messages.View(), xOffset)
}

func (m *Model) renderViewWithSecretOverlay() string {
	base := m.renderBaseView()
	ui := m.secretPromptUI()
	if ui == nil || !ui.active() {
		return base
	}

	xOffset := 0
	if m.input != nil {
		xOffset = m.input.CommandPickerXOffset()
	}

	messagesView := m.messages.View()
	anchor := lipgloss.Height(messagesView)
	return ui.render(base, anchor, xOffset)
}

func (m *Model) renderBaseView() string {
	const sidebarW = 60
	mainW := m.w
	if m.sidebarVisible {
		mainW = m.w - sidebarW
	}

	var headerView string
	if m.header != nil {
		headerWithStats := m.header.View()
		if !m.sidebarVisible {
			// Show stats when sidebar is closed
			statsView := m.stats.View()
			headerView = lipgloss.JoinHorizontal(lipgloss.Top, headerWithStats, statsView)
		} else {
			// Hide stats when sidebar is open
			headerView = headerWithStats
		}
	}

	headerHeight := 0
	if headerView != "" {
		headerHeight = lipgloss.Height(headerView) + 1 // +1 for newline spacing
	}
	m.messages.SetScreenTop(headerHeight)

	messagesView := m.messages.View()

	inputView := m.input.View()
	inputWithPadding := lipgloss.NewStyle().PaddingTop(1).PaddingBottom(1).Render(inputView)

	statusView := lipgloss.NewStyle().
		Width(mainW).
		MaxWidth(mainW).
		PaddingBottom(1).
		Render(m.status.View())

	var mainContent string
	if headerView != "" {
		mainContent = lipgloss.JoinVertical(lipgloss.Left, headerView+"\n", messagesView, inputWithPadding, statusView)
	} else {
		mainContent = lipgloss.JoinVertical(lipgloss.Left, messagesView, inputWithPadding, statusView)
	}

	var fullView string
	if m.sidebarVisible {
		sidebarView := m.sidebar.View()
		fullView = lipgloss.JoinHorizontal(lipgloss.Top, mainContent, sidebarView)
	} else {
		fullView = mainContent
	}
	theming := styles.CurrentTheme()
	base := theming.S().Base.Render(fullView)

	if overlay := m.input.CommandPickerView(); overlay != "" {
		overlayHeight := m.input.CommandPickerHeight()
		overlayY := headerHeight + lipgloss.Height(messagesView) + 1 - overlayHeight
		if overlayY < 0 {
			overlayY = 0
		}
		base = overlayString(base, overlay, m.input.CommandPickerXOffset(), overlayY)
	}

	if picker := m.agentPicker; picker != nil {
		overlay := picker.View()
		if overlay != "" {
			overlayHeight := picker.Height()
			overlayY := headerHeight + lipgloss.Height(messagesView) + 1 - overlayHeight
			if overlayY < 0 {
				overlayY = 0
			}
			base = overlayString(base, overlay, m.input.CommandPickerXOffset(), overlayY)
		}
	}

	return base
}

func overlayString(base, overlay string, x, y int) string {
	if overlay == "" {
		return base
	}

	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")

	required := y + len(overlayLines)
	for len(baseLines) < required {
		baseLines = append(baseLines, "")
	}

	for i, line := range overlayLines {
		idx := y + i
		if idx < 0 || idx >= len(baseLines) {
			continue
		}

		baseLine := baseLines[idx]

		left := ansi.Truncate(baseLine, x, "")
		leftWidth := lipgloss.Width(left)
		if leftWidth < x {
			left += strings.Repeat(" ", x-leftWidth)
		}

		overlayWidth := lipgloss.Width(line)
		right := ansi.TruncateLeft(baseLine, x+overlayWidth, "")

		baseLines[idx] = left + line + right
	}

	return strings.Join(baseLines, "\n")
}
