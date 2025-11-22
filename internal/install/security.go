package install

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"opperator/internal/agent"
	"opperator/internal/credentials"
	"tui/opper"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// SecurityFinding represents a potential security issue found during scanning
type SecurityFinding struct {
	File           string `json:"file"`
	Issue          string `json:"issue"`
	Severity       string `json:"severity"`
	Recommendation string `json:"recommendation"`
}

// SecurityScanResult contains the results of a security scan
type SecurityScanResult struct {
	Findings []SecurityFinding `json:"findings"`
	Verdict  string            `json:"verdict"`
	Notes    string            `json:"notes"`
}

// confirmTrustWarning shows a warning about installing untrusted code
func confirmTrustWarning() (bool, error) {
	confirm := false
	theme := createHuhTheme()

	fmt.Println() // Add margin top

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("⚠ This agent can execute code on your system").
				Description("Only install agents from sources you trust. Malicious agents could harm your system or steal data.").
				Value(&confirm).
				Affirmative("I trust this source").
				Negative("Cancel"),
		),
	).WithTheme(theme).WithShowErrors(false).WithShowHelp(false)
	if err := form.Run(); err != nil {
		return false, err
	}

	return confirm, nil
}

// confirmSecurityScan asks if the user wants to run a security scan
func confirmSecurityScan() (bool, error) {
	runScan := true
	theme := createHuhTheme()

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Run security scan? (Recommended)").
				Description("Scans agent code for potentially malicious patterns using AI.").
				Value(&runScan).
				Affirmative("Run scan").
				Negative("Skip"),
		),
	).WithTheme(theme).WithShowErrors(false).WithShowHelp(false)
	if err := form.Run(); err != nil {
		return false, err
	}

	return runScan, nil
}

// confirmProceedAfterFindings asks user if they want to proceed despite security findings
func confirmProceedAfterFindings() (bool, error) {
	confirm := false
	theme := createHuhTheme()

	fmt.Println() // Add margin top

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Proceed despite security findings?").
				Description("Security issues were detected. Installing this agent may be risky.").
				Value(&confirm).
				Affirmative("Proceed anyway").
				Negative("Cancel"),
		),
	).WithTheme(theme).WithShowErrors(false).WithShowHelp(false)
	if err := form.Run(); err != nil {
		return false, err
	}

	return confirm, nil
}

// confirmProceedAfterScanError asks user if they want to proceed when scan fails
func confirmProceedAfterScanError(scanErr error) (bool, error) {
	confirm := false
	theme := createHuhTheme()

	fmt.Println() // Add margin top

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Scan failed — continue without results?").
				Description(fmt.Sprintf("Security scan did not complete (%v).\nProceed to install anyway?", scanErr)).
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

// runSecurityScan performs a malware-focused security scan on the agent code
func runSecurityScan(agentDir string) (SecurityScanResult, error) {
	var result SecurityScanResult

	apiKey, err := credentials.GetSecret(credentials.OpperAPIKeyName)
	if err != nil {
		return result, fmt.Errorf("Opper API key not configured: %w", err)
	}

	payload, err := collectFilesForScan(agentDir, 120_000)
	if err != nil {
		return result, err
	}

	instructions := `You are a security analyst reviewing code for potentially malicious behavior. This code is being installed on a user's system, so focus on threats that could harm the user.

Look for:
- Reverse shells, backdoors, or remote access tools
- Unauthorized network connections or data exfiltration
- File system abuse (reading ~/.ssh, /etc/passwd, browser data, credentials)
- Cryptocurrency miners or resource hijacking
- Obfuscated or encoded code (base64 encoded commands, eval of strings)
- Suspicious exec/system/subprocess calls with user input
- Attempts to disable security features or logging
- Keyloggers or screen capture code
- Self-modifying or self-replicating code
- Downloading and executing remote code

Rules:
- Focus on HIGH-signal malicious patterns, not code quality issues
- Ignore legitimate use of subprocess/exec for agent functionality
- Consider context: an AI agent legitimately needs some system access
- Report only genuinely suspicious patterns that could harm the user

Respond as JSON with fields:
- findings: array of {file, issue, severity, recommendation}
- verdict: "safe" | "suspicious" | "malicious"
- notes: optional string (keep concise)`

	req := opper.StreamRequest{
		Name:         "opperator.malware_scan",
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
							"file":           map[string]any{"type": "string", "description": "Relative path of the file containing the issue"},
							"issue":          map[string]any{"type": "string", "description": "Description of the suspicious behavior"},
							"severity":       map[string]any{"type": "string", "description": "LOW|MEDIUM|HIGH"},
							"recommendation": map[string]any{"type": "string", "description": "How to address the issue"},
						},
					},
				},
				"verdict": map[string]any{"type": "string", "description": "safe|suspicious|malicious"},
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
		return result, fmt.Errorf("security scan failed: %w", err)
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

// RunSecurityScanWithSpinner runs the security scan with a spinner UI
func RunSecurityScanWithSpinner(agentDir string) (SecurityScanResult, error) {
	var (
		res SecurityScanResult
		err error
	)

	action := func() error {
		res, err = runSecurityScan(agentDir)
		return err
	}

	_ = RunStepWithSpinner("Scanning for malicious code...", action)

	return res, err
}

// PrintSecurityFindings displays the security scan results
func PrintSecurityFindings(res SecurityScanResult) {
	primary := lipgloss.Color("#f7c0af")
	fgMuted := lipgloss.Color("#b3b3b3")
	errorRed := lipgloss.Color("#bf5d47")
	warningYellow := lipgloss.Color("#ffc107")

	if len(res.Findings) == 0 {
		checkmark := lipgloss.NewStyle().Foreground(primary).MarginLeft(1).Render("✓")
		message := lipgloss.NewStyle().Foreground(fgMuted).Render(" No security issues detected")
		fmt.Println(checkmark + message)
		return
	}

	// Print warning header
	warningIcon := lipgloss.NewStyle().Foreground(warningYellow).MarginLeft(1).Render("⚠")
	warningMsg := lipgloss.NewStyle().Foreground(fgMuted).Render(fmt.Sprintf(" Found %d potential security issue(s):", len(res.Findings)))
	fmt.Println(warningIcon + warningMsg)
	fmt.Println()

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

		issueText := lipgloss.NewStyle().Foreground(fgMuted).MarginLeft(3).Render(f.Issue)
		fmt.Println(issueText)
		fmt.Println()
	}
}

// collectFilesForScan collects files from the agent directory for scanning
func collectFilesForScan(root string, budget int) (map[string]string, error) {
	files := map[string]string{}
	current := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		if info.IsDir() {
			if rel != "." && shouldSkipForScan(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldSkipForScan(rel) {
			return nil
		}
		if !isInspectableForScan(rel) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if current+len(data) > budget {
			return nil
		}
		current += len(data)
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	return files, err
}

// shouldSkipForScan determines if a path should be skipped during scanning
func shouldSkipForScan(rel string) bool {
	if agent.ShouldExcludePath(rel) {
		return true
	}
	// Additional scan-only ignores
	base := filepath.Base(rel)
	switch base {
	case ".git", ".env", ".ssh", ".docker", ".npmrc":
		return true
	}
	normalized := filepath.ToSlash(rel)
	if strings.HasPrefix(normalized, "build/") || strings.HasPrefix(normalized, "dist/") {
		return true
	}
	if strings.HasSuffix(base, ".egg-info") {
		return true
	}
	return false
}

// isInspectableForScan determines if a file should be included in the scan
func isInspectableForScan(rel string) bool {
	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".py", ".txt", ".md", ".json", ".yaml", ".yml", ".toml", ".sh", ".js", ".ts", ".go", ".rs", ".env", ".ini", ".cfg", ".bash", ".zsh":
		return true
	default:
		return false
	}
}
