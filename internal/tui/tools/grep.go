package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

//go:embed grep.md
var grepDescription []byte

const (
	GrepToolName   = "grep"
	grepMaxMatches = 200
)

type GrepParams struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path"`
	Include     string `json:"include"`
	LiteralText bool   `json:"literal_text"`
}

type GrepMetadata struct {
	Pattern   string `json:"pattern"`
	Root      string `json:"root"`
	Matches   int    `json:"matches"`
	Truncated bool   `json:"truncated"`
}

type grepMatch struct {
	path    string
	line    int
	content string
}

func GrepSpec() Spec {
	return Spec{
		Name:        GrepToolName,
		Description: strings.TrimSpace(string(grepDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":      map[string]any{"type": "string", "description": "Regular expression or literal text to search for"},
				"path":         map[string]any{"type": "string", "description": "Directory to search (defaults to workspace)"},
				"include":      map[string]any{"type": "string", "description": "Optional glob to limit files (e.g. *.go)"},
				"literal_text": map[string]any{"type": "boolean", "description": "Treat pattern as literal text instead of regex"},
			},
			"required": []string{"pattern"},
		},
	}
}

func RunGrep(ctx context.Context, arguments string, workingDir string) (string, string) {
	var params GrepParams
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

	var matcher func(string) bool
	if params.LiteralText {
		matcher = func(line string) bool { return strings.Contains(line, params.Pattern) }
	} else {
		re, err := regexp.Compile(params.Pattern)
		if err != nil {
			return fmt.Sprintf("error compiling pattern: %v", err), ""
		}
		matcher = re.MatchString
	}

	matches := make([]grepMatch, 0, 16)
	errStop := errors.New("stop search")

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.TrimSpace(params.Include) != "" {
			if ok, err := filepath.Match(params.Include, filepath.Base(path)); err == nil && !ok {
				return nil
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if !utf8.Valid(data) {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for idx, line := range lines {
			if matcher(line) {
				matches = append(matches, grepMatch{path: path, line: idx + 1, content: line})
				if len(matches) >= grepMaxMatches {
					return errStop
				}
			}
		}
		if ctx != nil && ctx.Err() != nil {
			return errStop
		}
		return nil
	})

	if err != nil && !errors.Is(err, errStop) {
		return fmt.Sprintf("error searching files: %v", err), ""
	}

	if len(matches) == 0 {
		return fmt.Sprintf("No matches for %q", params.Pattern), ""
	}

	var b strings.Builder
	for _, m := range matches {
		rel := m.path
		if relPath, err := filepath.Rel(root, m.path); err == nil {
			rel = relPath
		}
		fmt.Fprintf(&b, "%s:%d: %s\n", rel, m.line, truncateForDisplay(strings.TrimSpace(m.content), 120))
	}

	meta := GrepMetadata{Pattern: params.Pattern, Root: root, Matches: len(matches), Truncated: len(matches) >= grepMaxMatches}
	mb, _ := json.Marshal(meta)
	return strings.TrimSuffix(b.String(), "\n"), string(mb)
}
