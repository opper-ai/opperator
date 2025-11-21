package publish

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"opperator/config"
	"opperator/internal/agent"
)

func collectFiles(root string, budget int) (map[string]string, error) {
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
			if rel != "." && shouldSkip(rel) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldSkip(rel) {
			return nil
		}
		if !isInspectable(rel) {
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

func shouldSkip(rel string) bool {
	normalized := filepath.ToSlash(rel)
	if agent.ShouldExcludePath(rel) {
		return true
	}
	// Additional publish-only ignores
	base := filepath.Base(rel)
	switch base {
	case ".env", ".ssh", ".docker", ".npmrc":
		return true
	}
	if strings.HasPrefix(normalized, "build/") || strings.HasPrefix(normalized, "dist/") {
		return true
	}
	if strings.HasSuffix(base, ".egg-info") {
		return true
	}
	return false
}

func isInspectable(rel string) bool {
	normalized := filepath.ToSlash(rel)
	if strings.HasPrefix(normalized, "opperator/") {
		return false
	}

	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".py", ".txt", ".md", ".json", ".yaml", ".yml", ".toml", ".sh", ".js", ".ts", ".go", ".rs", ".env", ".ini", ".cfg":
		return true
	default:
		return false
	}
}

func copyAgent(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldSkip(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func writeGitignore(dir string) error {
	path := filepath.Join(dir, ".gitignore")
	entries := []string{
		".venv/",
		"__pycache__/",
		".pytest_cache/",
		".mypy_cache/",
		"node_modules/",
		"build/",
		"dist/",
		"*.egg-info",
		".DS_Store",
		".env",
		"*.pyc",
	}

	existing := map[string]bool{}
	lines := []string{}
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			existing[trimmed] = true
			lines = append(lines, trimmed)
		}
	}

	for _, e := range entries {
		if !existing[e] {
			lines = append(lines, e)
		}
	}

	content := strings.Join(lines, "\n")
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	return os.WriteFile(path, []byte(content), 0644)
}

func resolveAgentDir(name string) (string, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve config dir: %w", err)
	}
	return filepath.Join(configDir, "agents", name), nil
}
