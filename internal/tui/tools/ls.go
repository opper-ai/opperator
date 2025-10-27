package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed ls.md
var lsDescription []byte

const (
	LSToolName   = "ls"
	lsMaxEntries = 200
	lsSleepDelay = 1 * time.Millisecond
)

type LSParams struct {
	Path   string   `json:"path"`
	Ignore []string `json:"ignore"`
}

type LSMeta struct {
	Path       string `json:"path"`
	EntryCount int    `json:"entry_count"`
	Truncated  bool   `json:"truncated"`
}

func LSSpec() Spec {
	return Spec{
		Name:        LSToolName,
		Description: strings.TrimSpace(string(lsDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Directory to list (defaults to current working directory)",
				},
				"ignore": map[string]any{
					"type":        "array",
					"description": "Optional glob patterns to ignore",
					"items":       map[string]any{"type": "string"},
				},
			},
		},
	}
}

func RunLS(ctx context.Context, arguments string, workingDir string) (string, string) {
	if err := sleepWithCancel(ctx, lsSleepDelay); err != nil {
		return "canceled", ""
	}

	var params LSParams
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return fmt.Sprintf("error parsing parameters: %v", err), ""
	}

	dir, err := resolveWorkingPath(workingDir, params.Path)
	if err != nil {
		requested := strings.TrimSpace(params.Path)
		if requested == "" {
			requested = workingDir
		}
		return fmt.Sprintf("error listing %s: %v", requested, err), ""
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if strings.TrimSpace(params.Path) != "" {
			return fmt.Sprintf("error listing %s: %v", params.Path, err), ""
		}
		return fmt.Sprintf("error listing %s: %v", dir, err), ""
	}

	filtered := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if shouldIgnoreEntry(entry.Name(), params.Ignore) {
			continue
		}
		filtered = append(filtered, entry)
	}

	sort.Slice(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		if a.IsDir() != b.IsDir() {
			return a.IsDir()
		}
		return strings.ToLower(a.Name()) < strings.ToLower(b.Name())
	})

	var b strings.Builder
	fmt.Fprintf(&b, "Listing for %s:\n", dir)

	limit := lsMaxEntries
	if limit > len(filtered) {
		limit = len(filtered)
	}
	for i := 0; i < limit; i++ {
		entry := filtered[i]
		if entry.IsDir() {
			fmt.Fprintf(&b, "[DIR] %s/\n", entry.Name())
		} else {
			fmt.Fprintf(&b, "      %s\n", entry.Name())
		}
	}

	truncated := len(filtered) > lsMaxEntries
	if truncated {
		fmt.Fprintf(&b, "… %d more entries not shown\n", len(filtered)-lsMaxEntries)
	}

	meta := LSMeta{Path: dir, EntryCount: len(filtered), Truncated: truncated}
	mb, _ := json.Marshal(meta)
	return strings.TrimRight(b.String(), "\n"), string(mb)
}

func shouldIgnoreEntry(name string, patterns []string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if ok, err := filepath.Match(pattern, name); err == nil && ok {
			return true
		}
	}
	return false
}
