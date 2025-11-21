package publish

import (
	"bytes"
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"opperator/internal/agent"
)

//go:embed templates/AGENT_PUBLISH_README.md.tmpl
var readmeTemplate string

type readmeData struct {
	AgentName         string
	Description       string
	PackageName       string
	RepoName          string
	Secrets           []string
	HasDynamicSecrets bool
	AgentsYAML        string
}

func buildReadme(data readmeData) string {
	// Ensure description has a default
	if strings.TrimSpace(data.Description) == "" {
		data.Description = fmt.Sprintf("Automation agent packaged for Opperator. Repository: %s.", data.RepoName)
	}

	// Ensure agent name has a default
	if strings.TrimSpace(data.AgentName) == "" {
		data.AgentName = "Opperator Agent"
	}

	// Sort secrets for consistent output
	if len(data.Secrets) > 0 {
		secrets := append([]string{}, data.Secrets...)
		sort.Strings(secrets)
		data.Secrets = secrets
	}

	// Parse and execute template
	tmpl, err := template.New("readme").Parse(readmeTemplate)
	if err != nil {
		// Fallback to error message if template parsing fails
		return fmt.Sprintf("# Error generating README\n\nTemplate parsing failed: %v\n", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		// Fallback to error message if template execution fails
		return fmt.Sprintf("# Error generating README\n\nTemplate execution failed: %v\n", err)
	}

	return buf.String()
}

func buildAgentsYAMLSnippet(cfg *agent.AgentConfig) string {
	// Manually build a concise snippet to avoid dumping unused defaults.
	var b strings.Builder
	b.WriteString("agents:\n")
	b.WriteString("  - name: " + cfg.Name + "\n")
	b.WriteString("    command: \"" + cfg.Command + "\"\n")

	if len(cfg.Args) > 0 {
		b.WriteString("    args:\n")
		for _, arg := range cfg.Args {
			b.WriteString("      - \"" + escapeYAMLString(arg) + "\"\n")
		}
	}

	if cfg.ProcessRoot != "" {
		b.WriteString("    process_root: \"" + escapeYAMLString(cfg.ProcessRoot) + "\"\n")
	}

	if cfg.Description != "" {
		b.WriteString("    description: \"" + escapeYAMLString(cfg.Description) + "\"\n")
	}

	if cfg.Color != "" {
		b.WriteString("    color: \"" + escapeYAMLString(cfg.Color) + "\"\n")
	}

	if len(cfg.Env) > 0 {
		b.WriteString("    env:\n")
		keys := make([]string, 0, len(cfg.Env))
		for k := range cfg.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString("      " + k + ": \"" + escapeYAMLString(cfg.Env[k]) + "\"\n")
		}
	}

	if cfg.AutoRestart {
		b.WriteString("    auto_restart: true\n")
	}
	if cfg.MaxRestarts > 0 {
		b.WriteString(fmt.Sprintf("    max_restarts: %d\n", cfg.MaxRestarts))
	}
	if cfg.StartWithDaemon != nil {
		b.WriteString(fmt.Sprintf("    start_with_daemon: %t\n", *cfg.StartWithDaemon))
	}
	if cfg.SystemPrompt != "" {
		b.WriteString("    system_prompt: |\n")
		for _, line := range strings.Split(cfg.SystemPrompt, "\n") {
			b.WriteString("      " + line + "\n")
		}
	}

	return strings.TrimSpace(b.String())
}

func escapeYAMLString(s string) string {
	escaped := strings.ReplaceAll(s, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	return escaped
}
