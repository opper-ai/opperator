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

//go:embed write.md
var writeDescription []byte

const WriteToolName = "write"

type WriteParams struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type WriteMetadata struct {
	FilePath     string `json:"file_path"`
	AbsolutePath string `json:"absolute_path"`
	BytesWritten int    `json:"bytes_written"`
	WrittenAt    string `json:"written_at"`
	Created      bool   `json:"created"`
	Additions    int    `json:"additions,omitempty"`
	Removals     int    `json:"removals,omitempty"`
}

func WriteSpec() Spec {
	return Spec{
		Name:        WriteToolName,
		Description: strings.TrimSpace(string(writeDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{
					"type":        "string",
					"description": "Absolute or relative path to the file",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Full contents to write",
				},
			},
			"required": []string{"file_path", "content"},
		},
	}
}

func RunWrite(ctx context.Context, arguments string, workingDir string) (string, string) {
	if ctx != nil && ctx.Err() != nil {
		return "canceled", ""
	}

	var params WriteParams
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return fmt.Sprintf("error parsing parameters: %v", err), ""
	}

	if strings.TrimSpace(params.FilePath) == "" {
		return "error: file_path is required", ""
	}

	absPath, err := resolveWorkingPath(workingDir, params.FilePath)
	if err != nil {
		return fmt.Sprintf("error writing %s: %v", params.FilePath, err), ""
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Sprintf("error preparing directory: %v", err), ""
	}

	previous := ""
	if data, err := os.ReadFile(absPath); err == nil {
		previous = string(data)
		if previous == params.Content {
			return fmt.Sprintf("No changes needed for %s", params.FilePath), ""
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Sprintf("error reading %s: %v", params.FilePath, err), ""
	}

	if err := os.WriteFile(absPath, []byte(params.Content), 0o644); err != nil {
		return fmt.Sprintf("error writing %s: %v", params.FilePath, err), ""
	}

	recordFileWrite(absPath)
	recordFileRead(absPath)

	relPath := relativeToWorkspace(workingDir, absPath)
	_, additions, removals := diffStats(previous, params.Content, relPath)

	meta := WriteMetadata{
		FilePath:     params.FilePath,
		AbsolutePath: absPath,
		BytesWritten: len(params.Content),
		WrittenAt:    time.Now().Format(time.RFC3339),
		Created:      previous == "",
		Additions:    additions,
		Removals:     removals,
	}
	mb, _ := json.Marshal(meta)
	return fmt.Sprintf("Wrote %d bytes to %s", len(params.Content), params.FilePath), string(mb)
}
