package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed glob.md
var globDescription []byte

const (
	GlobToolName   = "glob"
	globSleepDelay = 1 * time.Millisecond
	globMaxResults = 200
)

type GlobParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

type GlobMetadata struct {
	Pattern   string `json:"pattern"`
	Root      string `json:"root"`
	Matches   int    `json:"matches"`
	Truncated bool   `json:"truncated"`
}

func GlobSpec() Spec {
	return Spec{
		Name:        GlobToolName,
		Description: strings.TrimSpace(string(globDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "Glob pattern to evaluate"},
				"path":    map[string]any{"type": "string", "description": "Root directory for the search (defaults to workspace)"},
			},
			"required": []string{"pattern"},
		},
	}
}

func RunGlob(ctx context.Context, arguments string, workingDir string) (string, string) {
	if err := sleepWithCancel(ctx, globSleepDelay); err != nil {
		return "canceled", ""
	}

	var params GlobParams
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return fmt.Sprintf("error parsing parameters: %v", err), ""
	}

	if strings.TrimSpace(params.Pattern) == "" {
		return "error: pattern is required", ""
	}

	root, err := resolveWorkingPath(workingDir, params.Path)
	if err != nil {
		requested := strings.TrimSpace(params.Path)
		if requested == "" {
			requested = workingDir
		}
		return fmt.Sprintf("error resolving path %s: %v", requested, err), ""
	}

	pattern := params.Pattern
	fullPattern := pattern
	if !filepath.IsAbs(pattern) {
		fullPattern = filepath.Join(root, pattern)
	}

	matches, err := filepath.Glob(fullPattern)
	if err != nil {
		return fmt.Sprintf("error matching pattern: %v", err), ""
	}

	sort.Strings(matches)
	truncated := false
	if len(matches) > globMaxResults {
		matches = matches[:globMaxResults]
		truncated = true
	}

	var b strings.Builder
	if len(matches) == 0 {
		b.WriteString("No files matched the pattern")
	} else {
		for _, match := range matches {
			rel := match
			if relPath, err := filepath.Rel(root, match); err == nil {
				rel = relPath
			}
			b.WriteString(rel)
			b.WriteString("\n")
		}
		if truncated {
			b.WriteString("… results truncated …\n")
		}
	}

	meta := GlobMetadata{Pattern: pattern, Root: root, Matches: len(matches), Truncated: truncated}
	mb, _ := json.Marshal(meta)
	return strings.TrimSuffix(b.String(), "\n"), string(mb)
}
