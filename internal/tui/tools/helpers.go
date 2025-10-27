package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	udiff "github.com/aymanbagabas/go-udiff"
)

var errPathOutsideWorkspace = errors.New("path outside allowed workspace")

func resolveWorkingPath(workingDir, candidate string) (string, error) {
	base := strings.TrimSpace(workingDir)
	if base == "" {
		base = "."
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		return absBase, nil
	}
	expanded, err := expandUserPath(trimmed)
	if err != nil {
		return "", err
	}
	trimmed = expanded
	var absPath string
	if filepath.IsAbs(trimmed) {
		absPath = filepath.Clean(trimmed)
	} else {
		absPath = filepath.Clean(filepath.Join(absBase, trimmed))
	}
	if !withinWorkspace(absBase, absPath) {
		return "", fmt.Errorf("%w: %s", errPathOutsideWorkspace, trimmed)
	}
	return absPath, nil
}

// ResolveWorkingPath exposes the workspace-aware path resolution for other packages.
func ResolveWorkingPath(workingDir, candidate string) (string, error) {
	return resolveWorkingPath(workingDir, candidate)
}

func withinWorkspace(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."
}

func expandUserPath(path string) (string, error) {
	if path == "" {
		return path, nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func sleepWithCancel(ctx context.Context, d time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// SleepWithCancel exposes the cancellation-aware sleep helper for callers in other packages.
func SleepWithCancel(ctx context.Context, d time.Duration) error {
	return sleepWithCancel(ctx, d)
}

func truncateForDisplay(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

func diffStats(before, after, fileName string) (string, int, int) {
	if before == after {
		return "", 0, 0
	}
	cleaned := strings.TrimSpace(fileName)
	cleaned = filepath.ToSlash(cleaned)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" {
		cleaned = "file"
	}
	diff := udiff.Unified("a/"+cleaned, "b/"+cleaned, before, after)
	additions, removals := 0, 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			additions++
		} else if strings.HasPrefix(line, "-") {
			removals++
		}
	}
	return diff, additions, removals
}

func relativeToWorkspace(workingDir, absPath string) string {
	base, err := resolveWorkingPath(workingDir, "")
	if err != nil {
		return filepath.ToSlash(absPath)
	}
	rel, err := filepath.Rel(base, absPath)
	if err != nil {
		return filepath.ToSlash(absPath)
	}
	if rel == "." {
		rel = filepath.Base(absPath)
	}
	return filepath.ToSlash(rel)
}
