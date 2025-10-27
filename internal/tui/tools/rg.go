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
	"sort"
	"strings"
	"unicode/utf8"
)

//go:embed rg.md
var rgDescription []byte

const (
	RGToolName = "rg"
	rgMaxFiles = 200
)

type RGParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Include string `json:"include"`
}

type RGMetadata struct {
	Pattern   string   `json:"pattern"`
	Root      string   `json:"root"`
	Matches   int      `json:"matches"`
	Truncated bool     `json:"truncated"`
	Files     []string `json:"files"`
}

func RGSpec() Spec {
	return Spec{
		Name:        RGToolName,
		Description: strings.TrimSpace(string(rgDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "Pattern to search for"},
				"path":    map[string]any{"type": "string", "description": "Directory to search (defaults to workspace)"},
				"include": map[string]any{"type": "string", "description": "Optional glob limiting files"},
			},
			"required": []string{"pattern"},
		},
	}
}

func RunRG(ctx context.Context, arguments string, workingDir string) (string, string) {
	var params RGParams
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

	re, err := regexp.Compile(params.Pattern)
	if err != nil {
		return fmt.Sprintf("error compiling pattern: %v", err), ""
	}

	files := make([]string, 0, 16)
	seen := make(map[string]struct{})
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
		if re.Match(data) {
			rel := path
			if relPath, err := filepath.Rel(root, path); err == nil {
				rel = relPath
			}
			if _, ok := seen[rel]; !ok {
				seen[rel] = struct{}{}
				files = append(files, rel)
			}
			if len(files) >= rgMaxFiles {
				return errStop
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

	sort.Strings(files)
	if len(files) == 0 {
		return fmt.Sprintf("No files matched pattern %q", params.Pattern), ""
	}

	var b strings.Builder
	for _, file := range files {
		b.WriteString(file)
		b.WriteString("\n")
	}

	meta := RGMetadata{Pattern: params.Pattern, Root: root, Matches: len(files), Truncated: len(files) >= rgMaxFiles, Files: files}
	mb, _ := json.Marshal(meta)
	return strings.TrimSuffix(b.String(), "\n"), string(mb)
}
