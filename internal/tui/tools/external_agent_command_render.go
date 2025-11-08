package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"

	"tui/internal/protocol"
	"tui/styles"
	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
)

func init() {
	registerExternalAgentCommandRenderer(externalAgentCommandDef{
		ToolName: AgentCommandToolName,
		Label:    "Agent Command",
	})
}

type externalAgentCommandMetadata struct {
	Agent   string          `json:"agent"`
	Command string          `json:"command"`
	Success *bool           `json:"success"`
	Error   string          `json:"error"`
	Result  json.RawMessage `json:"result"`
}

func registerExternalAgentCommandRenderer(def externalAgentCommandDef) {
	if def.Async {
		registerExternalAgentCommandAsyncRenderer(def)
		return
	}

	toolregistry.Register(def.ToolName, toolregistry.Definition{
		Label:  def.Label,
		Hidden: def.Hidden,
		Pending: func(call tooltypes.Call, width int, spinner string) string {
			return renderAgentCommandPending(def, width, spinner)
		},
		Render: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			return renderAgentCommandResult(def, call, result, width)
		},
		SummaryRender: func(call tooltypes.Call, result tooltypes.Result, width int) string {
			return renderAgentCommandSummary(def, result)
		},
		Copy: func(call tooltypes.Call, result tooltypes.Result) string {
			if trimmed := strings.TrimSpace(result.Content); trimmed != "" {
				return trimmed
			}
			return strings.TrimSpace(result.Metadata)
		},
	})
}

func renderAgentCommandPending(def externalAgentCommandDef, width int, spinner string) string {
	t := styles.CurrentTheme()
	meta := externalAgentCommandMetadata{}
	title := agentCommandHeaderTitle(def, meta)
	header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + title)
	if spinner != "" {
		header += " " + spinner
	}
	return truncateWidth(header, width)
}

func renderAgentCommandResult(def externalAgentCommandDef, call tooltypes.Call, result tooltypes.Result, width int) string {
	t := styles.CurrentTheme()
	meta, _ := parseAgentCommandMetadata(result.Metadata)
	title := agentCommandHeaderTitle(def, meta)

	header := lipgloss.NewStyle().Foreground(t.FgMuted).Render("└ " + title)

	indent := "  "
	gutter := lipgloss.NewStyle().MarginLeft(2).Foreground(t.FgMuted).Render("│ ")
	prefix := indent + gutter

	contentWidth := width
	if contentWidth <= 0 {
		contentWidth = 80
	}
	minWidth := lipgloss.Width(prefix) + 8
	if contentWidth < minWidth {
		contentWidth = minWidth
	}
	maxLine := contentWidth - lipgloss.Width(prefix)

	var builder strings.Builder
	builder.WriteString(header)

	statusText, statusStyle := agentCommandStatus(meta, result)
	wroteBody := false
	if statusText != "" {
		builder.WriteString("\n\n")
		builder.WriteString(prefix)
		builder.WriteString(truncateWidth(statusStyle.Render(statusText), maxLine))
		wroteBody = true
	}

	if args := agentCommandArgs(call.Input, def.Arguments); len(args) > 0 {
		if wroteBody {
			builder.WriteString("\n")
		} else {
			builder.WriteString("\n\n")
		}
		builder.WriteString(prefix)
		builder.WriteString(truncateWidth(lipgloss.NewStyle().Foreground(t.FgMuted).Render("Args"), maxLine))
		argStyle := t.S().Base.Foreground(t.FgBase)
		for _, arg := range args {
			builder.WriteString("\n")
			builder.WriteString(prefix)
			builder.WriteString(truncateWidth(argStyle.Render(arg), maxLine))
		}
		wroteBody = true
	}

	if lines := agentCommandResultLines(meta, result); len(lines) > 0 {
		if wroteBody {
			builder.WriteString("\n")
		} else {
			builder.WriteString("\n\n")
		}
		for i, line := range lines {
			if i > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString(prefix)
			builder.WriteString(truncateWidth(line, maxLine))
		}
	}

	return builder.String()
}

func renderAgentCommandSummary(def externalAgentCommandDef, result tooltypes.Result) string {
	meta, _ := parseAgentCommandMetadata(result.Metadata)
	base := agentCommandSummaryTitle(def, meta)

	if meta.Success != nil {
		if *meta.Success {
			return base
		}
		return base + " — failed"
	}

	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(result.Content)), "error") {
		return base + " — failed"
	}

	return base
}

func agentCommandStatus(meta externalAgentCommandMetadata, result tooltypes.Result) (string, lipgloss.Style) {
	t := styles.CurrentTheme()
	if meta.Success != nil {
		if *meta.Success {
			return "Succeeded", lipgloss.NewStyle().Foreground(t.Success).Bold(true)
		}
		return "Failed", lipgloss.NewStyle().Foreground(t.Error).Bold(true)
	}

	content := strings.TrimSpace(strings.ToLower(result.Content))
	switch {
	case strings.HasPrefix(content, "error"):
		return "Failed", lipgloss.NewStyle().Foreground(t.Error).Bold(true)
	case content != "":
		return "Completed", lipgloss.NewStyle().Foreground(t.Success).Bold(true)
	default:
		return "", lipgloss.NewStyle()
	}
}

func agentCommandResultLines(meta externalAgentCommandMetadata, result tooltypes.Result) []string {
	t := styles.CurrentTheme()
	detailStyle := t.S().Base.Foreground(t.FgBase)
	errorStyle := t.S().Base.Foreground(t.Error)

	if trimmed := strings.TrimSpace(result.Content); trimmed != "" {
		lines := strings.Split(trimmed, "\n")
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			out = append(out, detailStyle.Render(line))
		}
		return out
	}

	if len(meta.Result) > 0 && string(meta.Result) != "null" {
		var generic any
		if err := json.Unmarshal(meta.Result, &generic); err == nil {
			if pretty, err := json.MarshalIndent(generic, "", "  "); err == nil {
				lines := strings.Split(strings.TrimRight(string(pretty), "\n"), "\n")
				out := make([]string, 0, len(lines))
				for _, line := range lines {
					out = append(out, detailStyle.Render(line))
				}
				return out
			}
		}
	}

	if msg := strings.TrimSpace(meta.Error); msg != "" {
		return []string{errorStyle.Render(msg)}
	}

	if trimmed := strings.TrimSpace(result.Metadata); trimmed != "" {
		return []string{detailStyle.Render(trimmed)}
	}

	return nil
}

func agentCommandArgs(input string, schema []protocol.CommandArgument) []string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var payload struct {
		Args map[string]any `json:"args"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil || len(payload.Args) == 0 {
		return nil
	}

	if len(schema) == 0 {
		keys := make([]string, 0, len(payload.Args))
		for key := range payload.Args {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make([]string, 0, len(keys))
		for _, key := range keys {
			out = append(out, formatArgumentLine(key, "", payload.Args[key]))
		}
		return out
	}

	ordered := make([]string, 0, len(payload.Args))
	seen := make(map[string]struct{}, len(payload.Args))
	for _, arg := range schema {
		name := strings.TrimSpace(arg.Name)
		if name == "" {
			continue
		}
		value, ok := payload.Args[name]
		if !ok {
			continue
		}
		seen[name] = struct{}{}
		ordered = append(ordered, formatArgumentLine(name, arg.Type, value))
	}

	if len(seen) != len(payload.Args) {
		extra := make([]string, 0, len(payload.Args)-len(seen))
		for key := range payload.Args {
			if _, ok := seen[key]; !ok {
				extra = append(extra, key)
			}
		}
		sort.Strings(extra)
		for _, key := range extra {
			ordered = append(ordered, formatArgumentLine(key, "", payload.Args[key]))
		}
	}

	return ordered
}

func formatArgumentLine(name, typeName string, value any) string {
	label := name
	if trimmed := strings.TrimSpace(typeName); trimmed != "" {
		label = fmt.Sprintf("%s (%s)", name, trimmed)
	}
	if b, err := json.Marshal(value); err == nil {
		return fmt.Sprintf("%s: %s", label, string(b))
	}
	return fmt.Sprintf("%s: %v", label, value)
}

func parseAgentCommandMetadata(raw string) (externalAgentCommandMetadata, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return externalAgentCommandMetadata{}, false
	}
	var meta externalAgentCommandMetadata
	if err := json.Unmarshal([]byte(trimmed), &meta); err != nil {
		return externalAgentCommandMetadata{}, false
	}
	return meta, true
}

func agentCommandHeaderTitle(def externalAgentCommandDef, meta externalAgentCommandMetadata) string {
	// Prefer the command name from metadata if available
	label := strings.TrimSpace(meta.Command)
	if label != "" {
		// Prettify the command name
		label = toolregistry.PrettifyName(label)
	} else {
		label = strings.TrimSpace(def.Label)
		if label == "" {
			label = strings.TrimSpace(def.Command)
		}
		if label == "" {
			label = "Agent command"
		}
	}
	return label
}

func agentCommandSummaryTitle(def externalAgentCommandDef, meta externalAgentCommandMetadata) string {
	// Prefer the command name from metadata if available
	label := strings.TrimSpace(meta.Command)
	if label != "" {
		// Prettify the command name
		label = toolregistry.PrettifyName(label)
	} else {
		label = strings.TrimSpace(def.Label)
		if label == "" {
			label = strings.TrimSpace(def.Command)
		}
	}
	if label == "" {
		label = "Agent command"
	}

	agentName := strings.TrimSpace(meta.Agent)
	if agentName == "" {
		agentName = strings.TrimSpace(def.AgentName)
	}
	if agentName != "" {
		return fmt.Sprintf("%s (%s)", label, agentName)
	}
	return label
}
