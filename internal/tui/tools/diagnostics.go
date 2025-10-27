package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"

	"tui/lsp"
)

//go:embed diagnostics.md
var diagnosticsDescription []byte

const DiagnosticsToolName = "diagnostics"

type DiagnosticsParams struct {
	Path string `json:"path"`
}

type DiagnosticsMetadata struct {
	Path               string   `json:"path"`
	AbsolutePath       string   `json:"absolute_path,omitempty"`
	Generated          string   `json:"generated"`
	Status             string   `json:"status"`
	Reason             string   `json:"reason,omitempty"`
	Warnings           []string `json:"warnings,omitempty"`
	ClientCount        int      `json:"client_count,omitempty"`
	Clients            []string `json:"clients,omitempty"`
	FileDiagnostics    int      `json:"file_diagnostics,omitempty"`
	ProjectDiagnostics int      `json:"project_diagnostics,omitempty"`
}

func DiagnosticsSpec() Spec {
	return Spec{
		Name:        DiagnosticsToolName,
		Description: strings.TrimSpace(string(diagnosticsDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Optional file path to inspect"},
			},
		},
	}
}

func RunDiagnostics(ctx context.Context, arguments string, workingDir string, manager *lsp.Manager) (string, string) {
	if ctx != nil && ctx.Err() != nil {
		return "canceled", ""
	}

	var params DiagnosticsParams
	_ = json.Unmarshal([]byte(arguments), &params)

	meta := DiagnosticsMetadata{
		Path:      strings.TrimSpace(params.Path),
		Generated: time.Now().Format(time.RFC3339),
	}

	if manager == nil {
		meta.Status = "unavailable"
		meta.Reason = "lsp manager not configured"
		mb, _ := json.Marshal(meta)
		return "Diagnostics are not available because the LSP subsystem is disabled.", string(mb)
	}

	target := strings.TrimSpace(params.Path)
	abs := ""
	if target != "" {
		resolved, err := resolveWorkingPath(workingDir, target)
		if err != nil {
			meta.Status = "error"
			meta.Reason = err.Error()
			mb, _ := json.Marshal(meta)
			return fmt.Sprintf("error resolving diagnostics path %s: %v", target, err), string(mb)
		}
		abs = resolved
		meta.AbsolutePath = abs
	}

	clients, warnings := manager.ClientsForPath(ctx, abs)
	if len(warnings) > 0 {
		meta.Warnings = append(meta.Warnings, warnings...)
	}
	if len(clients) == 0 {
		meta.Status = "no_clients"
		mb, _ := json.Marshal(meta)
		return "No language servers are configured for diagnostics.", string(mb)
	}

	notifyCtx := ctx
	if notifyCtx == nil {
		notifyCtx = context.Background()
	}
	notifyErrs := make([]string, 0)
	for _, client := range clients {
		if abs == "" {
			client.WaitForDiagnostics(notifyCtx, 2*time.Second)
			continue
		}
		if err := client.OpenFileOnDemand(notifyCtx, abs); err != nil {
			notifyErrs = append(notifyErrs, fmt.Sprintf("%s: open: %v", client.GetName(), err))
			continue
		}
		if err := client.NotifyChange(notifyCtx, abs); err != nil {
			notifyErrs = append(notifyErrs, fmt.Sprintf("%s: change: %v", client.GetName(), err))
		}
		client.WaitForDiagnostics(notifyCtx, 5*time.Second)
	}
	if len(notifyErrs) > 0 {
		meta.Warnings = append(meta.Warnings, notifyErrs...)
	}

	fileDiags, projectDiags := collectDiagnosticsFromClients(clients, abs)
	clientNames := make([]string, 0, len(clients))
	for _, client := range clients {
		clientNames = append(clientNames, client.GetName())
	}
	sort.Strings(clientNames)

	meta.Status = "ok"
	meta.ClientCount = len(clients)
	meta.Clients = clientNames
	meta.FileDiagnostics = len(fileDiags)
	meta.ProjectDiagnostics = len(projectDiags)

	output := buildDiagnosticsOutput(fileDiags, projectDiags)
	if strings.TrimSpace(output) == "" {
		mb, _ := json.Marshal(meta)
		return "No diagnostics reported by active language servers.", string(mb)
	}

	mb, _ := json.Marshal(meta)
	return output, string(mb)
}

func collectDiagnosticsFromClients(clients []*lsp.Client, absPath string) (fileDiags, projectDiags []string) {
	canonical := normalisePath(absPath)
	for _, client := range clients {
		diagnostics := client.GetDiagnostics()
		for uri, diags := range diagnostics {
			path, err := uri.Path()
			if err != nil {
				continue
			}
			sameFile := canonical != "" && normalisePath(path) == canonical
			for _, diag := range diags {
				formatted := formatDiagnostic(path, diag, client.GetName())
				if sameFile {
					fileDiags = append(fileDiags, formatted)
				} else {
					projectDiags = append(projectDiags, formatted)
				}
			}
		}
	}
	sortDiagnostics(fileDiags)
	sortDiagnostics(projectDiags)
	return fileDiags, projectDiags
}

func buildDiagnosticsOutput(fileDiags, projectDiags []string) string {
	var b strings.Builder
	writeDiagnosticsSection(&b, "file_diagnostics", fileDiags)
	writeDiagnosticsSection(&b, "project_diagnostics", projectDiags)
	if len(fileDiags) > 0 || len(projectDiags) > 0 {
		fileErrors := countSeverity(fileDiags, "Error")
		fileWarnings := countSeverity(fileDiags, "Warn")
		projectErrors := countSeverity(projectDiags, "Error")
		projectWarnings := countSeverity(projectDiags, "Warn")
		b.WriteString("\n<diagnostic_summary>\n")
		fmt.Fprintf(&b, "Current file: %d errors, %d warnings\n", fileErrors, fileWarnings)
		fmt.Fprintf(&b, "Project: %d errors, %d warnings\n", projectErrors, projectWarnings)
		b.WriteString("</diagnostic_summary>\n")
	}
	return strings.TrimSpace(b.String())
}

func writeDiagnosticsSection(b *strings.Builder, tag string, diagnostics []string) {
	if len(diagnostics) == 0 {
		return
	}
	b.WriteString("\n<" + tag + ">\n")
	if len(diagnostics) > 10 {
		b.WriteString(strings.Join(diagnostics[:10], "\n"))
		fmt.Fprintf(b, "\n... and %d more diagnostics", len(diagnostics)-10)
	} else {
		b.WriteString(strings.Join(diagnostics, "\n"))
	}
	b.WriteString("\n</" + tag + ">\n")
}

func sortDiagnostics(diags []string) {
	sort.Slice(diags, func(i, j int) bool {
		iIsError := strings.HasPrefix(diags[i], "Error")
		jIsError := strings.HasPrefix(diags[j], "Error")
		if iIsError != jIsError {
			return iIsError
		}
		return diags[i] < diags[j]
	})
}

func countSeverity(diags []string, severity string) int {
	count := 0
	for _, diag := range diags {
		if strings.HasPrefix(diag, severity) {
			count++
		}
	}
	return count
}

func formatDiagnostic(path string, diagnostic protocol.Diagnostic, source string) string {
	severity := "Info"
	switch diagnostic.Severity {
	case protocol.SeverityError:
		severity = "Error"
	case protocol.SeverityWarning:
		severity = "Warn"
	case protocol.SeverityHint:
		severity = "Hint"
	case protocol.SeverityInformation:
		severity = "Info"
	}

	location := fmt.Sprintf("%s:%d:%d", path, diagnostic.Range.Start.Line+1, diagnostic.Range.Start.Character+1)

	sourceInfo := source
	if diagnostic.Source != "" {
		sourceInfo = diagnostic.Source
	}

	codeInfo := ""
	if diagnostic.Code != nil {
		codeInfo = fmt.Sprintf("[%v]", diagnostic.Code)
	}

	tagsInfo := ""
	if len(diagnostic.Tags) > 0 {
		tags := make([]string, 0, len(diagnostic.Tags))
		for _, tag := range diagnostic.Tags {
			switch tag {
			case protocol.Unnecessary:
				tags = append(tags, "unnecessary")
			case protocol.Deprecated:
				tags = append(tags, "deprecated")
			}
		}
		if len(tags) > 0 {
			tagsInfo = fmt.Sprintf(" (%s)", strings.Join(tags, ", "))
		}
	}

	message := strings.TrimSpace(diagnostic.Message)
	if message == "" {
		message = "<no message>"
	}

	return fmt.Sprintf("%s: %s [%s]%s%s %s", severity, location, sourceInfo, codeInfo, tagsInfo, message)
}
