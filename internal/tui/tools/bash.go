package tools

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed bash.md
var bashDescription []byte

const (
	BashToolName       = "bash"
	bashDefaultTimeout = 60 * time.Second
	bashMaxTimeout     = 10 * time.Minute
	bashMaxOutputRunes = 4000
	BashNoOutput       = "no output"
)

type BashParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

type BashMetadata struct {
	Command          string `json:"command"`
	StartTime        string `json:"start_time"`
	EndTime          string `json:"end_time"`
	WorkingDirectory string `json:"working_directory"`
	TimedOut         bool   `json:"timed_out"`
	ExitError        string `json:"exit_error,omitempty"`
	OutputTruncated  bool   `json:"output_truncated"`
}

var bannedCommands = []string{
	"apt", "apt-get", "aptitude", "yum", "dnf", "rpm", "pacman", "brew",
	"curl", "wget", "scp", "ssh", "telnet", "nc", "netcat", "sudo", "su", "chmod", "chown",
}

var bannedCommandRegexp = compileBannedCommandRegexp()

func compileBannedCommandRegexp() *regexp.Regexp {
	escaped := make([]string, len(bannedCommands))
	for i, cmd := range bannedCommands {
		escaped[i] = regexp.QuoteMeta(cmd)
	}
	pattern := `(?m)^(?:sudo|su)\b|\b(?:` + strings.Join(escaped, "|") + `)\b`
	return regexp.MustCompile(pattern)
}

func BashSpec() Spec {
	return Spec{
		Name:        BashToolName,
		Description: strings.TrimSpace(string(bashDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "Command to execute"},
				"timeout": map[string]any{"type": "integer", "description": "Optional timeout in milliseconds"},
			},
			"required": []string{"command"},
		},
	}
}

func RunBash(ctx context.Context, arguments string, workingDir string) (string, string) {
	var params BashParams
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return fmt.Sprintf("error parsing parameters: %v", err), ""
	}

	command := strings.TrimSpace(params.Command)
	if command == "" {
		return "error: command is required", ""
	}

	if msg := validateBashCommand(command, workingDir); msg != "" {
		return msg, ""
	}

	timeout := bashDefaultTimeout
	if params.Timeout > 0 {
		requested := time.Duration(params.Timeout) * time.Millisecond
		if requested < time.Second {
			requested = time.Second
		}
		if requested > bashMaxTimeout {
			requested = bashMaxTimeout
		}
		timeout = requested
	}

	execCtx := ctx
	if execCtx == nil {
		execCtx = context.Background()
	}
	execCtx, cancel := context.WithTimeout(execCtx, timeout)
	defer cancel()

	shell := "bash"
	if _, err := exec.LookPath(shell); err != nil {
		shell = "sh"
	}

	start := time.Now()
	cmd := exec.CommandContext(execCtx, shell, "-lc", command)
	cmd.Dir = workingDir

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout

	err := cmd.Run()
	end := time.Now()

	meta := BashMetadata{
		Command:          command,
		StartTime:        start.Format(time.RFC3339),
		EndTime:          end.Format(time.RFC3339),
		WorkingDirectory: workingDir,
	}

	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		meta.TimedOut = true
		meta.ExitError = "timeout"
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("command timed out after %s", timeout), string(mb)
	}

	output := strings.TrimRight(stdout.String(), "\n")
	if output == "" {
		output = BashNoOutput
	}

	runes := []rune(output)
	if len(runes) > bashMaxOutputRunes {
		output = string(runes[:bashMaxOutputRunes]) + "\n… output truncated …"
		meta.OutputTruncated = true
	}

	if err != nil {
		meta.ExitError = err.Error()
		if output == BashNoOutput {
			output = fmt.Sprintf("error: %v", err)
		} else {
			output = fmt.Sprintf("%s\n(error: %v)", output, err)
		}
	}

	mb, _ := json.Marshal(meta)
	return output, string(mb)
}

func validateBashCommand(command, workingDir string) string {
	fields := strings.Fields(command)
	first := firstNonAssignmentToken(fields)
	if isUvExecutable(first) {
		if allowed, allowedRoot := isWorkingDirUnderConfig(workingDir); !allowed {
			return fmt.Sprintf("error: uv commands are only permitted inside %s", allowedRoot)
		}
	}

	if bannedCommandRegexp.MatchString(command) {
		return "error: command contains disallowed operations"
	}

	return ""
}

func firstNonAssignmentToken(tokens []string) string {
	for _, token := range tokens {
		if isShellAssignment(token) {
			continue
		}
		return token
	}
	return ""
}

func isShellAssignment(token string) bool {
	return strings.ContainsRune(token, '=') && !strings.HasPrefix(token, "=")
}

func isUvExecutable(token string) bool {
	if token == "" {
		return false
	}
	trimmed := strings.Trim(token, "'\"")
	base := filepath.Base(trimmed)
	return base == "uv" || base == "uv.exe"
}

func isWorkingDirUnderConfig(workingDir string) (bool, string) {
	root := configRoot()
	if root == "" {
		return false, "~/.config/opperator"
	}

	if workingDir == "" {
		return false, root
	}

	expanded, err := expandUserPath(workingDir)
	if err != nil {
		return false, root
	}

	if expanded == "" {
		return false, root
	}

	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return false, root
	}

	rootClean := filepath.Clean(root)
	targetClean := filepath.Clean(absolute)
	if rootClean == targetClean {
		return true, root
	}

	rel, err := filepath.Rel(rootClean, targetClean)
	if err != nil {
		return false, root
	}

	if rel == "." {
		return true, root
	}

	if strings.HasPrefix(rel, "..") {
		return false, root
	}

	return true, root
}

func configRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.config/opperator"
	}
	return filepath.Join(home, ".config", "opperator")
}
