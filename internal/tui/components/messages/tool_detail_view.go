package messages

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"tui/internal/message"
	"tui/styles"
	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
	"tui/toolstate"
)

type ToolDetailView struct {
	log            *Messages
	call           tooltypes.Call
	result         tooltypes.Result
	width, height  int
	agentName      string
	taskDefinition string
}

func NewToolDetailView() *ToolDetailView {
	return &ToolDetailView{log: &Messages{}}
}

// SetFrameSize configures the outer frame size and adjusts the internal log.
func (v *ToolDetailView) SetFrameSize(width, height int) {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	v.width, v.height = width, height

	innerW := preferSize(width, 40, 88, 10)
	innerH := preferSize(height, 12, 32, 6)
	v.log.SetSize(innerW, innerH)
}

// SetData replaces the current transcript with the provided call details.
func (v *ToolDetailView) SetData(call tooltypes.Call, result tooltypes.Result) tea.Cmd {
	v.call = call
	v.result = result
	metadata := toolstate.ParseMetadata(result.Metadata)
	v.agentName, v.taskDefinition = toolstate.ExtractAgentMeta(metadata)

	loadConversation(v.log, nil)

	appender := newDetailAppender(v)

	for _, entry := range toolstate.TranscriptEntries(metadata) {
		role, label, body := convertTranscriptEntry(entry).detail()
		appender.add(role, label, body)
	}

	for _, entry := range toolstate.ProgressEntries(metadata) {
		label, body := convertProgressEntry(entry).detail()
		appender.add(message.Assistant, label, body)
	}

	appender.add(message.Assistant, "Result", result.Content)

	if !appender.hasContent() {
		appender.add(message.Assistant, "", "(no additional details)")
	}
	return nil
}

func (v *ToolDetailView) Update(msg tea.Msg) tea.Cmd {
	if v.log == nil {
		return nil
	}
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		v.SetFrameSize(m.Width, m.Height)
		return nil
	default:
		return v.log.Update(msg)
	}
}

func (v *ToolDetailView) View() string {
	if v.log == nil {
		return ""
	}
	body := v.log.View()
	t := styles.CurrentTheme()
	box := t.S().Base.Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocus).
		Padding(1, 2)
	if v.log.w > 0 {
		box = box.Width(min(v.log.w+4, max(v.width-2, 1)))
	}
	modal := box.Render(body)
	return lipgloss.Place(v.width, v.height, lipgloss.Center, lipgloss.Center, modal)
}

func (v *ToolDetailView) appendMessage(role message.Role, content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	cmp := newMessageCmp(role, content, v.log.w, false)
	v.log.appendItem(cmp)
}

type detailAppender struct {
	view     *ToolDetailView
	appended bool
}

func newDetailAppender(view *ToolDetailView) *detailAppender {
	return &detailAppender{view: view}
}

func (a *detailAppender) add(role message.Role, label, body string) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return
	}
	var builder strings.Builder
	if labelText := formatDetailLabel(label); labelText != "" {
		builder.WriteString(labelText)
		builder.WriteString("\n")
	}
	builder.WriteString(indentBlock(trimmed))
	a.view.appendMessage(role, builder.String())
	a.appended = true
}

func (a *detailAppender) hasContent() bool {
	return a.appended
}


type transcriptEntry struct {
	Kind               string
	Status             string
	Content            string
	ToolName           string
	ToolInput          string
	ToolResultContent  string
	ToolResultMetadata string
}

func convertTranscriptEntry(e toolstate.TranscriptEntry) transcriptEntry {
	return transcriptEntry{
		Kind:               e.Kind,
		Status:             e.Status,
		Content:            e.Content,
		ToolName:           e.ToolName,
		ToolInput:          e.ToolInput,
		ToolResultContent:  e.ToolResultContent,
		ToolResultMetadata: e.ToolResultMetadata,
	}
}

func (e transcriptEntry) summarize() string {
	kind := strings.ToLower(strings.TrimSpace(e.Kind))
	status := strings.ToLower(strings.TrimSpace(e.Status))
	switch kind {
	case "tool":
		name := toolDisplayName(e.ToolName)
		labelParts := []string{strings.TrimSpace(name)}
		if status != "" {
			labelParts = append(labelParts, status)
		}
		var builder strings.Builder
		builder.WriteString(strings.Join(filterNonEmpty(labelParts), " — "))
		if input := strings.TrimSpace(e.ToolInput); input != "" {
			builder.WriteString("\n")
			builder.WriteString(indentBlock(prettyJSON(input)))
		}
		if output := strings.TrimSpace(e.ToolResultContent); output != "" {
			builder.WriteString("\n")
			builder.WriteString(indentBlock(output))
		}
		if meta := strings.TrimSpace(e.ToolResultMetadata); meta != "" {
			builder.WriteString("\n")
			builder.WriteString(indentBlock(meta))
		}
		return strings.TrimSpace(builder.String())
	case "assistant", "user":
		content := strings.TrimSpace(e.Content)
		if content == "" {
			content = strings.TrimSpace(e.ToolResultContent)
		}
		if content == "" {
			return ""
		}
		return strings.Title(kind) + "\n" + indentBlock(content)
	default:
		content := strings.TrimSpace(e.Content)
		if content == "" {
			return ""
		}
		label := strings.TrimSpace(strings.Title(kind))
		if status != "" {
			label += " — " + status
		}
		return label + "\n" + indentBlock(content)
	}
}

type progressEntry struct {
	Timestamp string
	Status    string
	Text      string
	Metadata  string
}

func convertProgressEntry(e toolstate.ProgressEntry) progressEntry {
	return progressEntry{
		Timestamp: e.Timestamp,
		Status:    e.Status,
		Text:      e.Text,
		Metadata:  e.Metadata,
	}
}

func toolDisplayName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "Tool Call"
	}
	pretty := strings.TrimSpace(toolregistry.PrettifyName(trimmed))
	if pretty == "" {
		return trimmed
	}
	return pretty
}

func formatSection(title, content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "\n") {
		return fmt.Sprintf("%s:\n%s", title, indentBlock(trimmed))
	}
	return fmt.Sprintf("%s: %s", title, trimmed)
}

func indentBlock(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return strings.Join(lines, "\n")
}

func formatDetailLabel(label string) string {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("**%s**", trimmed)
}

func prettyJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var js any
	if json.Unmarshal([]byte(trimmed), &js) == nil {
		if b, err := json.MarshalIndent(js, "", "  "); err == nil {
			return string(b)
		}
	}
	return raw
}

func preferSize(total, minVal, maxVal, padding int) int {
	size := total - padding
	if maxVal > 0 && size > maxVal {
		size = maxVal
	}
	limit := total - 2
	if limit > 0 && size > limit {
		size = limit
	}
	if size < minVal {
		size = minVal
	}
	if size < 1 {
		size = 1
	}
	return size
}

func (e transcriptEntry) detail() (message.Role, string, string) {
	kind := strings.ToLower(strings.TrimSpace(e.Kind))
	status := strings.TrimSpace(e.Status)
	switch kind {
	case "user":
		body := strings.TrimSpace(e.Content)
		if body == "" {
			body = strings.TrimSpace(e.ToolResultContent)
		}
		return message.User, "User", body
	case "assistant":
		body := strings.TrimSpace(e.Content)
		if body == "" {
			body = strings.TrimSpace(e.ToolResultContent)
		}
		label := "Assistant"
		if status != "" {
			label = label + " (" + strings.Title(status) + ")"
		}
		return message.Assistant, label, body
	case "tool":
		name := toolDisplayName(e.ToolName)
		labelParts := []string{"Tool", name}
		if strings.TrimSpace(status) != "" {
			labelParts = append(labelParts, strings.Title(strings.TrimSpace(status)))
		}
		label := strings.Join(filterNonEmpty(labelParts), " • ")
		var segments []string
		if input := strings.TrimSpace(e.ToolInput); input != "" {
			segments = append(segments, formatSection("Input", prettyJSON(input)))
		}
		if output := strings.TrimSpace(e.ToolResultContent); output != "" {
			segments = append(segments, formatSection("Output", output))
		}
		if meta := strings.TrimSpace(e.ToolResultMetadata); meta != "" {
			segments = append(segments, formatSection("Metadata", meta))
		}
		if len(segments) == 0 {
			segments = append(segments, strings.TrimSpace(e.Content))
		}
		return message.Assistant, label, strings.Join(filterNonEmpty(segments), "\n\n")
	default:
		body := strings.TrimSpace(e.Content)
		label := strings.Title(kind)
		if status != "" {
			label += " (" + strings.Title(status) + ")"
		}
		return message.Assistant, label, body
	}
}

func (p progressEntry) detail() (string, string) {
	label := "Progress"
	if status := strings.TrimSpace(p.Status); status != "" {
		label += " (" + strings.Title(status) + ")"
	}
	if ts := strings.TrimSpace(p.Timestamp); ts != "" {
		label += " @ " + ts
	}
	var parts []string
	if text := strings.TrimSpace(p.Text); text != "" {
		parts = append(parts, text)
	}
	if meta := strings.TrimSpace(p.Metadata); meta != "" {
		parts = append(parts, meta)
	}
	return label, strings.Join(filterNonEmpty(parts), "\n\n")
}

func filterNonEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			trimmed = append(trimmed, value)
		}
	}
	return trimmed
}
