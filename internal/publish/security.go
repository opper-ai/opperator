package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"opperator/internal/credentials"
	"tui/opper"

	"github.com/charmbracelet/lipgloss"
)

type securityFinding struct {
	File           string `json:"file"`
	Issue          string `json:"issue"`
	Severity       string `json:"severity"`
	Recommendation string `json:"recommendation"`
}

type securityScanResult struct {
	Findings []securityFinding `json:"findings"`
	Verdict  string            `json:"verdict"`
	Notes    string            `json:"notes"`
}

func runSecurityScan(agentDir string) (securityScanResult, error) {
	var result securityScanResult

	apiKey, err := credentials.GetSecret(credentials.OpperAPIKeyName)
	if err != nil {
		return result, fmt.Errorf("Opper API key not configured: %w", err)
	}

	payload, err := collectFiles(agentDir, 120_000)
	if err != nil {
		return result, err
	}

	instructions := `You are a security auditor. Only look for leaked secrets/credentials (API keys, tokens, passwords, signing keys, cloud credentials, private keys, hardcoded secrets). Ignore other code quality issues (e.g., path traversal, command injection, file permissions).

Rules:
- Do NOT report on non-secret issues.
- Ignore any vendored SDK files under an "opperator/" subdirectory.
- Focus on high-signal evidence of secrets (hardcoded strings, default tokens, test keys, embedded credentials).

Respond as JSON with fields:
- findings: array of {file, issue, severity, recommendation}
- verdict: "safe" | "risky"
- notes: optional string (keep concise)`

	req := opper.StreamRequest{
		Name:         "opperator.security_audit",
		Instructions: &instructions,
		Input: map[string]any{
			"files": payload,
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"findings": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"file":           map[string]any{"type": "string", "description": "Relative path of the file containing the leak"},
							"issue":          map[string]any{"type": "string", "description": "Secret leak description"},
							"severity":       map[string]any{"type": "string", "description": "LOW|MEDIUM|HIGH"},
							"recommendation": map[string]any{"type": "string", "description": "How to remediate the leak"},
						},
					},
				},
				"verdict": map[string]any{"type": "string", "description": "safe|risky"},
				"notes":   map[string]any{"type": "string", "description": "Optional brief notes"},
			},
		},
		Model: "gcp/gemini-3-pro-preview",
	}

	client := opper.New(apiKey)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	events, err := client.Stream(ctx, req)
	if err != nil {
		return result, fmt.Errorf("LLM scan failed: %w", err)
	}

	agg := opper.NewJSONChunkAggregator()
	for evt := range events {
		if evt.Data.JSONPath != "" || evt.Data.ChunkType == "json" {
			agg.Add(evt.Data.JSONPath, evt.Data.Delta)
		}
	}

	body, err := agg.Assemble()
	if err != nil {
		return result, fmt.Errorf("failed to assemble scan output: %w", err)
	}
	if strings.TrimSpace(body) == "" {
		return result, nil
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return result, fmt.Errorf("failed to parse scan response: %w", err)
	}
	return result, nil
}

func runSecurityScanWithSpinner(agentDir string) (securityScanResult, error) {
	var (
		res securityScanResult
		err error
	)

	action := func() error {
		res, err = runSecurityScan(agentDir)
		return err
	}

	// Run a spinner to keep UI consistent with the wizard aesthetic.
	_ = runStepWithSpinner("Running code security scan...", action)

	return res, err
}

func printFindings(res securityScanResult) {
	primary := lipgloss.Color("#f7c0af")
	fgMuted := lipgloss.Color("#b3b3b3")
	errorRed := lipgloss.Color("#bf5d47")
	warningYellow := lipgloss.Color("#ffc107")

	if len(res.Findings) == 0 {
		checkmark := lipgloss.NewStyle().Foreground(primary).MarginLeft(1).Render("✓")
		message := lipgloss.NewStyle().Foreground(fgMuted).Render(" No secret leaks detected.")
		fmt.Println(checkmark + message)
		return
	}

	// Print each finding with consistent styling
	for _, f := range res.Findings {
		severity := strings.ToUpper(strings.TrimSpace(f.Severity))
		if severity == "" {
			severity = "INFO"
		}

		// Color code severity
		var severityColor lipgloss.Color
		switch severity {
		case "HIGH":
			severityColor = errorRed
		case "MEDIUM":
			severityColor = warningYellow
		default:
			severityColor = fgMuted
		}

		// Format severity badge
		severityBadge := lipgloss.NewStyle().Foreground(severityColor).Bold(true).Render(severity)
		fileName := lipgloss.NewStyle().Foreground(primary).Render(f.File)

		// Print finding with consistent indentation
		fmt.Print(lipgloss.NewStyle().MarginLeft(1).Foreground(fgMuted).Render("• "))
		fmt.Print(severityBadge + " ")
		fmt.Println(fileName)

		issueText := lipgloss.NewStyle().Foreground(fgMuted).Render("  " + f.Issue)
		fmt.Println(issueText)
		fmt.Println()
	}

	// Tip section with Builder agent reference
	secondary := lipgloss.Color("#3ccad7") // cyan for Builder
	tipLabel := lipgloss.NewStyle().Foreground(primary).MarginLeft(1).Render("Tip: ")
	tipText := lipgloss.NewStyle().Foreground(fgMuted).Render("Use the ")
	builderText := lipgloss.NewStyle().Foreground(secondary).Bold(true).Render("Builder")
	tipEnd := lipgloss.NewStyle().MarginBottom(1).Foreground(fgMuted).Render(" agent to resolve this issue")
	result := lipgloss.NewStyle().Render(tipLabel + tipText + builderText + tipEnd)
	fmt.Println(result)
}
