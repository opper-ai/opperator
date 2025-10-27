package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

//go:embed view.md
var viewDescription []byte

const (
	ViewToolName     = "view"
	viewDefaultLimit = 2000
	viewReadDelay    = 0 * time.Second
)

type ViewParams struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

type ViewMetadata struct {
	FilePath string `json:"file_path"`
	ReadAt   string `json:"read_at"`
}

func ViewSpec() Spec {
	return Spec{
		Name:        ViewToolName,
		Description: strings.TrimSpace(string(viewDescription)),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{
					"type":        "string",
					"description": "The path to the file to read",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Optional starting line (0-based)",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Optional number of lines to read",
				},
			},
			"required": []string{"file_path"},
		},
	}
}

func RunView(ctx context.Context, arguments string, workingDir string) (string, string) {
	if err := sleepWithCancel(ctx, viewReadDelay); err != nil {
		return "canceled", ""
	}

	var params ViewParams
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return fmt.Sprintf("error parsing parameters: %v", err), ""
	}

	if strings.TrimSpace(params.FilePath) == "" {
		return "error: file_path is required", ""
	}

	if params.Limit <= 0 {
		params.Limit = viewDefaultLimit
	}

	absPath, err := resolveWorkingPath(workingDir, params.FilePath)
	if err != nil {
		return fmt.Sprintf("error reading %s: %v", strings.TrimSpace(params.FilePath), err), ""
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("error reading %s: %v", params.FilePath, err), ""
	}

	lines := strings.Split(string(data), "\n")
	if params.Offset < 0 {
		params.Offset = 0
	}
	if params.Offset > len(lines) {
		params.Offset = len(lines)
	}
	end := params.Offset + params.Limit
	if end > len(lines) {
		end = len(lines)
	}
	slice := lines[params.Offset:end]
	content := strings.Join(slice, "\n")

	meta := ViewMetadata{
		FilePath: params.FilePath,
		ReadAt:   time.Now().Format(time.RFC3339),
	}
	mb, _ := json.Marshal(meta)
	recordFileRead(absPath)
	return content, string(mb)
}
