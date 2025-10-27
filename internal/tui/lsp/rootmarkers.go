package lsp

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// HasRootMarkers reports whether any of the marker patterns exist beneath dir.
func HasRootMarkers(dir string, rootMarkers []string) bool {
	if len(rootMarkers) == 0 {
		return true
	}
	cleaned := strings.TrimSpace(dir)
	if cleaned == "" {
		cleaned = "."
	}
	for _, pattern := range rootMarkers {
		pat := strings.TrimSpace(pattern)
		if pat == "" {
			continue
		}
		glob := filepath.Join(cleaned, pat)
		matches, err := doublestar.FilepathGlob(glob)
		if err != nil {
			continue
		}
		if len(matches) > 0 {
			return true
		}
	}
	return false
}
