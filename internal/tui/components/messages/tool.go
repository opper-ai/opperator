package messages

import (
	"fmt"
	"strings"
	"time"

	"tui/components/anim"
	"tui/styles"
	tooling "tui/tools"
	toolregistry "tui/tools/registry"
	"tui/toolstate"
	"tui/util"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
)

type ToolCallCmp interface {
	tea.Model
	tea.ViewModel
	SetSize(w, h int) tea.Cmd
	Focus() tea.Cmd
	Blur() tea.Cmd
	IsFocused() bool
	SetEntry(toolstate.Execution)
	Entry() toolstate.Execution
	Animating() bool
}

type toolCallCmp struct {
	width   int
	focused bool

	entry           toolstate.Execution
	spinner         *anim.Anim
	spinnerSettings anim.Settings
	spinning        bool
	savedPending    string
}

func NewToolCallCmp(entry toolstate.Execution) ToolCallCmp {
	t := styles.CurrentTheme()

	thinkingTexts := []string{
		"Executing...",
		"Calling tool...",
		"One second...",
	}

	settings := anim.Settings{
		Label:           thinkingTexts[time.Now().UnixNano()%int64(len(thinkingTexts))],
		Size:            50,
		LabelColor:      t.FgBase,
		GradColorA:      t.Primary,
		GradColorB:      t.Secondary,
		CycleColors:     true,
		BuildLabel:      true,
		BuildInterval:   50 * time.Millisecond,
		BuildDelay:      300 * time.Millisecond,
		ShufflePrelude:  1500 * time.Millisecond,
		ShowEllipsis:    false,
		CycleReveal:     true,
		DisplayDuration: 4 * time.Second,
	}

	cmp := &toolCallCmp{
		entry:           entry,
		spinner:         anim.New(settings),
		spinnerSettings: settings,
	}
	cmp.SetEntry(entry)
	return cmp
}

func (m *toolCallCmp) Init() tea.Cmd {
	if m.spinning && m.spinner != nil {
		return m.spinner.Init()
	}
	return nil
}

func (m *toolCallCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case anim.StepMsg:
		if m.spinning && m.spinner != nil {
			u, cmd := m.spinner.Update(msg)
			if a, ok := u.(*anim.Anim); ok {
				m.spinner = a
			}
			return m, cmd
		}
	case tea.KeyPressMsg:
		if m.focused && key.Matches(msg, CopyKey) {
			copyText := m.entry.Result.Content
			if def, ok := resolveToolDefinition(m.entry); ok && def.Copy != nil {
				copyText = def.Copy(m.entry.Call, m.entry.Result)
			}
			return m, tea.Sequence(
				tea.SetClipboard(copyText),
				func() tea.Msg {
					_ = clipboard.WriteAll(copyText)
					return nil
				},
				util.ReportInfo("Tool content copied to clipboard"),
			)
		}
	}
	return m, nil
}

func (m *toolCallCmp) View() string {
	// Safety check: if tool is marked as hidden, return empty view
	def, hasDef := resolveToolDefinition(m.entry)
	if hasDef && def.Hidden {
		return ""
	}

	t := styles.CurrentTheme()
	leftOnly := lipgloss.Border{Left: "▌"}
	style := t.S().Text
	if m.focused {
		style = style.PaddingLeft(1).BorderLeft(true).BorderStyle(leftOnly).BorderForeground(t.Secondary)
	} else {
		style = style.PaddingLeft(2)
	}

	call := m.entry.Call
	result := m.entry.Result

	if m.spinning {
		label := executionLabel(m.entry)
		if hasDef && def.Label != "" {
			label = def.Label
		}
		status := executionStatus(m.entry)
		spin := ""
		if m.spinning && m.spinner != nil {
			spin = m.spinner.View()
		}
		header := strings.TrimSpace(fmt.Sprintf("└ %s – %s", label, status))
		content := strings.TrimSpace(fmt.Sprintf("%s %s", header, spin))
		if hasDef {
			width := max(m.width-2, 1)
			if def.PendingWithResult != nil {
				content = def.PendingWithResult(call, result, width, spin)
			} else if def.Pending != nil {
				content = def.Pending(call, width, spin)
			}
		}
		if status := m.permissionStatus(); status != "" {
			trimmed := strings.TrimSpace(content)
			if trimmed == "" {
				content = fmt.Sprintf("%s %s", label, status)
			} else {
				content = fmt.Sprintf("%s %s", trimmed, status)
			}
		}
		m.savedPending = content
		return style.Width(max(m.width, 1)).Render(content)
	}

	width := max(m.width-2, 1)
	content := ""
	if hasDef {
		switch {
		case def.Render != nil:
			content = def.Render(call, result, width)
		case strings.TrimSpace(m.savedPending) != "" && def.PendingWithResult != nil:
			content = m.savedPending
		default:
			content = defaultFinishedRender(m.entry, width)
		}
	} else {
		content = defaultFinishedRender(m.entry, width)
	}
	return style.Width(max(m.width, 1)).Render(strings.TrimSpace(content))
}

func (m *toolCallCmp) Focus() tea.Cmd             { m.focused = true; return nil }
func (m *toolCallCmp) Blur() tea.Cmd              { m.focused = false; return nil }
func (m *toolCallCmp) IsFocused() bool            { return m.focused }
func (m *toolCallCmp) Animating() bool            { return m.spinning }
func (m *toolCallCmp) Entry() toolstate.Execution { return m.entry }
func (m *toolCallCmp) SetSize(w, _ int) tea.Cmd   { m.width = w; return nil }

func (m *toolCallCmp) SetEntry(entry toolstate.Execution) {
	prevSpinning := m.spinning
	m.entry = entry
	m.spinning = shouldSpin(entry)
	if m.spinning && !prevSpinning {
		m.spinner = anim.New(m.spinnerSettings)
	}

	if entry.Flags.Async {
		label := strings.TrimSpace(executionLabel(entry))
		lines := asyncProgressLines(entry)
		tooling.UpdateAsyncProgressSnapshot(entry.Call.ID, label, lines, entry.Finished())
	}
}

func (m *toolCallCmp) permissionStatus() string {
	switch m.entry.Permission {
	case toolstate.PermissionDenied:
		return "(permission denied)"
	case toolstate.PermissionRequested:
		return "(permission needed)"
	default:
		return ""
	}
}

func shouldSpin(entry toolstate.Execution) bool {
	if entry.Finished() {
		return false
	}
	if entry.Permission == toolstate.PermissionDenied {
		return false
	}
	if entry.Permission == toolstate.PermissionRequested {
		return true
	}
	switch entry.Lifecycle {
	case toolstate.LifecyclePending, toolstate.LifecycleRunning:
		return true
	case toolstate.LifecycleUnknown:
		if !entry.Call.Finished {
			return true
		}
	}
	if entry.Flags.Async && !entry.Call.Finished {
		return true
	}
	return false
}

func resolveToolDefinition(entry toolstate.Execution) (toolregistry.Definition, bool) {
	if trimmed := strings.TrimSpace(entry.Tool); trimmed != "" {
		if def, ok := toolregistry.Lookup(trimmed); ok {
			return def, true
		}
	}
	if entry.Flags.Async {
		if def, ok := toolregistry.Lookup(tooling.AsyncRenderToolName); ok {
			return def, true
		}
	}
	if trimmed := strings.TrimSpace(entry.Call.Name); trimmed != "" {
		if def, ok := toolregistry.Lookup(trimmed); ok {
			return def, true
		}
	}
	if trimmed := strings.TrimSpace(entry.Result.Name); trimmed != "" {
		if def, ok := toolregistry.Lookup(trimmed); ok {
			return def, true
		}
	}
	if entry.Call.Name == tooling.AsyncRenderToolName || entry.Result.Name == tooling.AsyncRenderToolName {
		if def, ok := toolregistry.Lookup(tooling.AsyncRenderToolName); ok {
			return def, true
		}
	}
	return toolregistry.Definition{}, false
}

func defaultFinishedRender(entry toolstate.Execution, width int) string {
	meta := toolstate.ParseMetadata(entry.Result.Metadata)
	label := strings.TrimSpace(preferredLabel(entry, meta))
	status := executionStatus(entry)

	fallback := executionLabel(entry)
	if label == "" {
		label = fallback
	}

	header := ""
	if label != "" {
		header = fmt.Sprintf("└ %s – %s", label, status)
	} else {
		header = fmt.Sprintf("Status: %s", status)
	}

	body := buildResultBody(entry, meta)
	parts := make([]string, 0, 2)
	if strings.TrimSpace(header) != "" {
		parts = append(parts, strings.TrimSpace(header))
	}
	if strings.TrimSpace(body) != "" {
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n")
}

func buildResultBody(entry toolstate.Execution, meta map[string]any) string {
	if len(entry.Display.Body) > 0 {
		return strings.Join(entry.Display.Body, "\n")
	}

	lines := make([]string, 0, 8)
	seen := make(map[string]struct{})

	push := func(values ...string) {
		for _, v := range values {
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			lines = append(lines, trimmed)
		}
	}

	push(entry.Display.Summary)
	push(entry.Result.Content)
	for _, progress := range entry.Progress {
		push(progress.Text)
		push(progress.Status)
		push(progress.Metadata)
	}
	if meta != nil {
		push(extractMetadataString(meta, "summary", "result_summary"))
		for _, entry := range toolstate.TranscriptEntries(meta) {
			push(entry.Content)
		}
		for _, entry := range toolstate.ProgressEntries(meta) {
			push(entry.Text)
		}
	}

	return strings.Join(lines, "\n")
}

func preferredLabel(entry toolstate.Execution, meta map[string]any) string {
	if trimmed := strings.TrimSpace(entry.Display.Label); trimmed != "" {
		return trimmed
	}
	if label := deriveLabelFromMetadata(meta); label != "" {
		return label
	}
	if trimmed := strings.TrimSpace(entry.Tool); trimmed != "" {
		return toolregistry.PrettifyName(trimmed)
	}
	if trimmed := strings.TrimSpace(entry.Result.Name); trimmed != "" {
		return toolregistry.PrettifyName(trimmed)
	}
	if trimmed := strings.TrimSpace(entry.Call.Name); trimmed != "" {
		return toolregistry.PrettifyName(trimmed)
	}
	return strings.TrimSpace(entry.Call.ID)
}

func deriveLabelFromMetadata(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	if label := extractMetadataString(meta, "label"); label != "" {
		return label
	}
	if ctx := extractMetadataMap(meta, "async_context"); len(ctx) > 0 {
		if label := extractMetadataString(ctx, "label", "title", "name"); label != "" {
			return label
		}
	}
	if nested := extractNestedMetadata(meta, "async_task_metadata", "task", "context"); len(nested) > 0 {
		if label := deriveLabelFromMetadata(nested); label != "" {
			return label
		}
	}
	if label := extractMetadataString(meta, "title", "tool_name", "name"); label != "" {
		return label
	}
	return ""
}

func extractMetadataString(meta map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := lookupMetadataValue(meta, key); ok {
			if str := stringifyMetadataValue(value); str != "" {
				return str
			}
		}
	}
	return ""
}

func extractMetadataMap(meta map[string]any, key string) map[string]any {
	if value, ok := lookupMetadataValue(meta, key); ok {
		if m, ok := value.(map[string]any); ok {
			return m
		}
		if raw, ok := value.(string); ok {
			if parsed := toolstate.ParseMetadata(raw); len(parsed) > 0 {
				return parsed
			}
		}
	}
	return nil
}

func extractNestedMetadata(meta map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if nested := extractMetadataMap(meta, key); len(nested) > 0 {
			return nested
		}
	}
	return nil
}

func lookupMetadataValue(meta map[string]any, key string) (any, bool) {
	if meta == nil {
		return nil, false
	}
	if value, ok := meta[key]; ok {
		return value, true
	}
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	for k, v := range meta {
		if strings.ToLower(strings.TrimSpace(k)) == lowerKey {
			return v, true
		}
	}
	return nil, false
}

func stringifyMetadataValue(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	case []byte:
		return strings.TrimSpace(string(t))
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func executionLabel(entry toolstate.Execution) string {
	if trimmed := strings.TrimSpace(entry.Display.Label); trimmed != "" {
		return trimmed
	}
	if entry.Flags.Async {
		if label := strings.TrimSpace(tooling.PreferredAsyncLabel(entry.Call, entry.Result)); label != "" {
			return label
		}
	}
	if trimmed := strings.TrimSpace(entry.Tool); trimmed != "" {
		return toolregistry.PrettifyName(trimmed)
	}
	if trimmed := strings.TrimSpace(entry.Result.Name); trimmed != "" {
		return toolregistry.PrettifyName(trimmed)
	}
	if trimmed := strings.TrimSpace(entry.Call.Name); trimmed != "" {
		return toolregistry.PrettifyName(trimmed)
	}
	return strings.TrimSpace(entry.Call.ID)
}

func executionStatus(entry toolstate.Execution) string {
	switch entry.Lifecycle {
	case toolstate.LifecyclePending:
		return "Pending"
	case toolstate.LifecycleRunning:
		return "Running"
	case toolstate.LifecycleCompleted:
		if entry.Result.IsError {
			return "Failed"
		}
		return "Completed"
	case toolstate.LifecycleFailed:
		return "Failed"
	case toolstate.LifecycleCancelled:
		return "Cancelled"
	case toolstate.LifecycleDeleted:
		return "Deleted"
	case toolstate.LifecycleUnknown:
		if entry.Result.IsError {
			return "Failed"
		}
		if entry.Call.Finished {
			return "Completed"
		}
		if entry.Flags.Async {
			return "Running"
		}
		if strings.TrimSpace(entry.Result.Content) != "" || len(entry.Progress) > 0 {
			return "Running"
		}
		return "Pending"
	default:
		if entry.Result.IsError {
			return "Failed"
		}
		if entry.Call.Finished {
			return "Completed"
		}
		return "Running"
	}
}

func asyncProgressLines(entry toolstate.Execution) []string {
	if len(entry.Progress) == 0 {
		return nil
	}
	lines := make([]string, 0, len(entry.Progress))
	for _, record := range entry.Progress {
		if line := formatProgressRecord(record); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	if len(lines) > tooling.MaxAsyncProgressLines {
		lines = append([]string(nil), lines[len(lines)-tooling.MaxAsyncProgressLines:]...)
	}
	return lines
}

func formatProgressRecord(record toolstate.ProgressRecord) string {
	text := strings.TrimSpace(record.Text)
	status := strings.TrimSpace(record.Status)
	metadata := strings.TrimSpace(record.Metadata)
	switch {
	case text != "" && status != "":
		return fmt.Sprintf("%s — %s", status, text)
	case text != "":
		return text
	case status != "":
		return status
	case metadata != "":
		return metadata
	default:
		if !record.Timestamp.IsZero() {
			return record.Timestamp.Format(time.RFC3339)
		}
		return ""
	}
}
