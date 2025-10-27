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

//go:embed multiedit.md
var multieditDescription []byte

const MultiEditToolName = "multiedit"

type MultiEditOperation struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

type MultiEditParams struct {
	FilePath string               `json:"file_path"`
	Edits    []MultiEditOperation `json:"edits"`
}

type MultiEditMetadata struct {
	FilePath     string `json:"file_path"`
	AbsolutePath string `json:"absolute_path"`
	EditsApplied int    `json:"edits_applied"`
	WrittenAt    string `json:"written_at"`
	Diff         string `json:"diff,omitempty"`
	Additions    int    `json:"additions,omitempty"`
	Removals     int    `json:"removals,omitempty"`
}

func MultiEditSpec() Spec {
	return Spec{
		Name:        MultiEditToolName,
		Description: strings.TrimSpace(string(multieditDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string", "description": "File to modify"},
				"edits": map[string]any{
					"type":        "array",
					"description": "List of edit operations to apply sequentially",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"old_string":  map[string]any{"type": "string", "description": "Text to replace"},
							"new_string":  map[string]any{"type": "string", "description": "Replacement text"},
							"replace_all": map[string]any{"type": "boolean", "description": "Replace every occurrence (defaults to false)"},
						},
						"required":             []string{"old_string", "new_string"},
						"additionalProperties": false,
					},
				},
			},
			"required": []string{"file_path", "edits"},
		},
	}
}

func RunMultiEdit(ctx context.Context, arguments string, workingDir string) (string, string) {
	if ctx != nil && ctx.Err() != nil {
		return "canceled", ""
	}

	var params MultiEditParams
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return fmt.Sprintf("error parsing parameters: %v", err), ""
	}

	if strings.TrimSpace(params.FilePath) == "" {
		return "error: file_path is required", ""
	}
	if len(params.Edits) == 0 {
		return "error: at least one edit is required", ""
	}

	absPath, err := resolveWorkingPath(workingDir, params.FilePath)
	if err != nil {
		return fmt.Sprintf("error editing %s: %v", params.FilePath, err), ""
	}

	data, err := os.ReadFile(absPath)
	content := ""
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Sprintf("error reading %s: %v", params.FilePath, err), ""
		}
	} else {
		content = string(data)
	}

	original := content
	applied := 0
	for idx, edit := range params.Edits {
		if strings.TrimSpace(edit.OldString) == "" {
			if idx == 0 {
				content = edit.NewString
				applied++
				continue
			}
			return fmt.Sprintf("error: edit %d has empty old_string", idx+1), ""
		}
		if strings.TrimSpace(content) == "" && idx == 0 {
			return fmt.Sprintf("error: file %s is empty; first edit must create content", params.FilePath), ""
		}
		if edit.ReplaceAll {
			count := strings.Count(content, edit.OldString)
			if count == 0 {
				continue
			}
			content = strings.ReplaceAll(content, edit.OldString, edit.NewString)
			applied += count
		} else {
			if !strings.Contains(content, edit.OldString) {
				continue
			}
			content = strings.Replace(content, edit.OldString, edit.NewString, 1)
			applied++
		}
	}

	if applied == 0 {
		return fmt.Sprintf("No edits applied to %s", params.FilePath), ""
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Sprintf("error preparing directory: %v", err), ""
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return fmt.Sprintf("error writing %s: %v", params.FilePath, err), ""
	}

	recordFileWrite(absPath)
	recordFileRead(absPath)

	relPath := relativeToWorkspace(workingDir, absPath)
	diff, additions, removals := diffStats(original, content, relPath)

	meta := MultiEditMetadata{
		FilePath:     params.FilePath,
		AbsolutePath: absPath,
		EditsApplied: applied,
		WrittenAt:    time.Now().Format(time.RFC3339),
		Diff:         diff,
		Additions:    additions,
		Removals:     removals,
	}
	mb, _ := json.Marshal(meta)
	return fmt.Sprintf("Applied %d edit operation(s) to %s", applied, params.FilePath), string(mb)
}
