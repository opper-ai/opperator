package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed edit.md
var editDescription []byte

const EditToolName = "edit"

type EditParams struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

type EditMetadata struct {
	FilePath     string `json:"file_path"`
	AbsolutePath string `json:"absolute_path"`
	Replacements int    `json:"replacements"`
	WrittenAt    string `json:"written_at"`
	Diff         string `json:"diff,omitempty"`
	Additions    int    `json:"additions,omitempty"`
	Removals     int    `json:"removals,omitempty"`
}

func EditSpec() Spec {
	return Spec{
		Name:        EditToolName,
		Description: strings.TrimSpace(string(editDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path":   map[string]any{"type": "string", "description": "File to modify"},
				"old_string":  map[string]any{"type": "string", "description": "Text to replace"},
				"new_string":  map[string]any{"type": "string", "description": "Replacement text"},
				"replace_all": map[string]any{"type": "boolean", "description": "Replace every occurrence (defaults to false)"},
			},
			"required": []string{"file_path", "old_string", "new_string"},
		},
	}
}

func RunEdit(ctx context.Context, arguments string, workingDir string) (string, string) {
	if ctx != nil && ctx.Err() != nil {
		return "canceled", ""
	}

	var params EditParams
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return fmt.Sprintf("error parsing parameters: %v", err), ""
	}

	if strings.TrimSpace(params.FilePath) == "" {
		return "error: file_path is required", ""
	}

	absPath, err := resolveWorkingPath(workingDir, params.FilePath)
	if err != nil {
		return fmt.Sprintf("error editing %s: %v", params.FilePath, err), ""
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Sprintf("error reading %s: %v", params.FilePath, err), ""
		}
		if strings.TrimSpace(params.OldString) != "" {
			return fmt.Sprintf("error reading %s: %v", params.FilePath, err), ""
		}
		content := params.NewString
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Sprintf("error preparing directory: %v", err), ""
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			return fmt.Sprintf("error writing %s: %v", params.FilePath, err), ""
		}
		recordFileWrite(absPath)
		recordFileRead(absPath)
		relPath := relativeToWorkspace(workingDir, absPath)
		diff, additions, removals := diffStats("", content, relPath)
		meta := EditMetadata{
			FilePath:     params.FilePath,
			AbsolutePath: absPath,
			Replacements: 1,
			WrittenAt:    time.Now().Format(time.RFC3339),
			Diff:         diff,
			Additions:    additions,
			Removals:     removals,
		}
		mb, _ := json.Marshal(meta)
		return fmt.Sprintf("Created %s", params.FilePath), string(mb)
	}

	content := string(data)
	original := content
	replacements := 0

	if strings.TrimSpace(params.OldString) == "" {
		if content == params.NewString {
			return fmt.Sprintf("No changes needed for %s", params.FilePath), ""
		}
		content = params.NewString
		replacements = 1
	} else if params.ReplaceAll {
		count := strings.Count(content, params.OldString)
		if count == 0 {
			return fmt.Sprintf("No matches for %s", truncateForDisplay(params.OldString, 32)), ""
		}
		content = strings.ReplaceAll(content, params.OldString, params.NewString)
		replacements = count
	} else {
		if !strings.Contains(content, params.OldString) {
			return fmt.Sprintf("No matches for %s", truncateForDisplay(params.OldString, 32)), ""
		}
		content = strings.Replace(content, params.OldString, params.NewString, 1)
		replacements = 1
	}

	if content == original {
		return fmt.Sprintf("No changes applied to %s", params.FilePath), ""
	}

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return fmt.Sprintf("error writing %s: %v", params.FilePath, err), ""
	}

	recordFileWrite(absPath)
	recordFileRead(absPath)

	relPath := relativeToWorkspace(workingDir, absPath)
	diff, additions, removals := diffStats(original, content, relPath)

	meta := EditMetadata{
		FilePath:     params.FilePath,
		AbsolutePath: absPath,
		Replacements: replacements,
		WrittenAt:    time.Now().Format(time.RFC3339),
		Diff:         diff,
		Additions:    additions,
		Removals:     removals,
	}
	mb, _ := json.Marshal(meta)
	return fmt.Sprintf("Applied %d replacement(s) to %s", replacements, params.FilePath), string(mb)
}
