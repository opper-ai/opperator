package tools

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
	"tui/styles"
	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
)

const AsyncRenderToolName = "async"

func init() {
	toolregistry.Register(AsyncRenderToolName, AsyncToolDefinition("Async"))
}

// The label parameter allows customizing the default label (e.g., "Agent", "Command").
func AsyncToolDefinition(defaultLabel string) toolregistry.Definition {
	label := strings.TrimSpace(defaultLabel)
	if label == "" {
		label = "Async"
	}

	normalizeLabel := func(vm AsyncViewModel, call tooltypes.Call, result tooltypes.Result) AsyncViewModel {
		trimmed := strings.TrimSpace(vm.Label)
		if trimmed == "" || isFallbackAsyncLabel(trimmed, call, result) {
			if preferred := strings.TrimSpace(PreferredAsyncLabel(call, result)); preferred != "" {
				trimmed = preferred
			}
		}
		vm.Label = strings.TrimSpace(preferDefinitionLabel(trimmed, call, result))
		if vm.Label == "" || strings.EqualFold(vm.Label, "Async") {
			vm.Label = label
		}
		return vm
	}

	return toolregistry.Definition{
		Label: label,
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			vm := globalAsyncManager.GetViewModel(call, tooltypes.Result{})
			vm = normalizeLabel(vm, call, tooltypes.Result{})
			return renderAsyncViewModel(vm, spinner, width)
		},
		PendingWithResult: func(call tooltypes.Call, result tooltypes.Result, width int, spinner string) string {
			vm := globalAsyncManager.GetViewModel(call, result)
			vm = normalizeLabel(vm, call, result)
			return renderAsyncViewModel(vm, spinner, width)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			vm := globalAsyncManager.GetViewModel(call, result)
			vm = normalizeLabel(vm, call, result)
			return renderAsyncViewModel(vm, "", width)
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			vm := globalAsyncManager.GetViewModel(call, result)
			vm = normalizeLabel(vm, call, result)
			return renderAsyncSummary(vm, width)
		},
		Copy: func(call tooltypes.Call, result tooltypes.Result) string {
			return strings.TrimSpace(result.Content)
		},
	}
}

// UpdateAsyncProgressSnapshot allows external callers to push progress updates.
func UpdateAsyncProgressSnapshot(callID, label string, lines []string, finished bool) {
	globalAsyncManager.UpdateSnapshot(callID, label, lines, finished)
}

// renderAsyncViewModel is the main pure rendering function.
// It takes a view model and returns formatted output with no side effects.
func renderAsyncViewModel(vm AsyncViewModel, spinner string, width int) string {
	t := styles.CurrentTheme()

	label := strings.TrimSpace(vm.Label)
	if label == "" {
		label = "Async"
	}
	status := strings.TrimSpace(vm.Status)
	if status == "" {
		status = "Running"
	}

	header := fmt.Sprintf("%s - %s", label, status)
	if vm.ShowSpinner && strings.TrimSpace(spinner) != "" {
		header = fmt.Sprintf("%s %s", header, spinner)
	}

	headerView := lipgloss.NewStyle().
		Foreground(t.FgMuted).
		Render("└ " + header)

	if len(vm.Lines) == 0 {
		return headerView
	}

	body := renderGutterList(vm.Lines, width, nil)
	if strings.TrimSpace(body) == "" {
		return headerView
	}

	return headerView + "\n\n" + body
}

// renderAsyncSummary creates a concise one-line summary.
func renderAsyncSummary(vm AsyncViewModel, width int) string {
	label := strings.TrimSpace(vm.Label)
	if label == "" {
		label = "Async"
	}
	status := strings.TrimSpace(vm.Status)
	if status == "" {
		status = "Running"
	}

	summary := fmt.Sprintf("%s - %s", label, status)

	if width <= 0 {
		width = 60
	}

	return shortenText(summary, width)
}
