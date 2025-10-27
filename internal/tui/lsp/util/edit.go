package util

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// ApplyWorkspaceEdit applies the provided workspace edit to the local file system.
func ApplyWorkspaceEdit(edit protocol.WorkspaceEdit) error {
	for uri, textEdits := range edit.Changes {
		if err := applyTextEdits(uri, textEdits); err != nil {
			return fmt.Errorf("apply text edits: %w", err)
		}
	}
	for _, change := range edit.DocumentChanges {
		if err := applyDocumentChange(change); err != nil {
			return fmt.Errorf("apply document change: %w", err)
		}
	}
	return nil
}

func applyTextEdits(uri protocol.DocumentURI, edits []protocol.TextEdit) error {
	path, err := uri.Path()
	if err != nil {
		return fmt.Errorf("invalid URI: %w", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	lineEnding := "\n"
	if bytes.Contains(content, []byte("\r\n")) {
		lineEnding = "\r\n"
	}
	endsWithNewline := len(content) > 0 && bytes.HasSuffix(content, []byte(lineEnding))
	lines := strings.Split(string(content), lineEnding)

	for i := range edits {
		for j := i + 1; j < len(edits); j++ {
			if rangesOverlap(edits[i].Range, edits[j].Range) {
				return fmt.Errorf("overlapping edits detected")
			}
		}
	}

	sorted := make([]protocol.TextEdit, len(edits))
	copy(sorted, edits)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Range.Start.Line != sorted[j].Range.Start.Line {
			return sorted[i].Range.Start.Line > sorted[j].Range.Start.Line
		}
		return sorted[i].Range.Start.Character > sorted[j].Range.Start.Character
	})

	for _, edit := range sorted {
		updated, err := applyTextEdit(lines, edit)
		if err != nil {
			return err
		}
		lines = updated
	}

	var builder strings.Builder
	for i, line := range lines {
		if i > 0 {
			builder.WriteString(lineEnding)
		}
		builder.WriteString(line)
	}
	if endsWithNewline && !strings.HasSuffix(builder.String(), lineEnding) {
		builder.WriteString(lineEnding)
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func applyTextEdit(lines []string, edit protocol.TextEdit) ([]string, error) {
	startLine := int(edit.Range.Start.Line)
	endLine := int(edit.Range.End.Line)
	startChar := int(edit.Range.Start.Character)
	endChar := int(edit.Range.End.Character)

	if startLine < 0 || startLine >= len(lines) {
		return nil, fmt.Errorf("invalid start line: %d", startLine)
	}
	if endLine < 0 || endLine >= len(lines) {
		endLine = len(lines) - 1
	}

	result := make([]string, 0, len(lines))
	result = append(result, lines[:startLine]...)

	startLineContent := lines[startLine]
	if startChar < 0 || startChar > len(startLineContent) {
		startChar = len(startLineContent)
	}
	prefix := startLineContent[:startChar]

	endLineContent := lines[endLine]
	if endChar < 0 || endChar > len(endLineContent) {
		endChar = len(endLineContent)
	}
	suffix := endLineContent[endChar:]

	if edit.NewText == "" {
		if prefix+suffix != "" {
			result = append(result, prefix+suffix)
		}
	} else {
		newLines := strings.Split(edit.NewText, "\n")
		if len(newLines) == 1 {
			result = append(result, prefix+newLines[0]+suffix)
		} else {
			result = append(result, prefix+newLines[0])
			result = append(result, newLines[1:len(newLines)-1]...)
			result = append(result, newLines[len(newLines)-1]+suffix)
		}
	}

	if endLine+1 < len(lines) {
		result = append(result, lines[endLine+1:]...)
	}
	return result, nil
}

func applyDocumentChange(change protocol.DocumentChange) error {
	if change.CreateFile != nil {
		path, err := change.CreateFile.URI.Path()
		if err != nil {
			return err
		}
		if change.CreateFile.Options != nil {
			if change.CreateFile.Options.Overwrite {
				// allow overwrite
			} else if change.CreateFile.Options.IgnoreIfExists {
				if _, err := os.Stat(path); err == nil {
					return nil
				}
			}
		}
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			return fmt.Errorf("create file: %w", err)
		}
	}

	if change.DeleteFile != nil {
		path, err := change.DeleteFile.URI.Path()
		if err != nil {
			return err
		}
		if change.DeleteFile.Options != nil && change.DeleteFile.Options.Recursive {
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("remove directory: %w", err)
			}
		} else if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove file: %w", err)
		}
	}

	if change.RenameFile != nil {
		oldPath, err := change.RenameFile.OldURI.Path()
		if err != nil {
			return err
		}
		newPath, err := change.RenameFile.NewURI.Path()
		if err != nil {
			return err
		}
		if change.RenameFile.Options != nil && !change.RenameFile.Options.Overwrite {
			if _, err := os.Stat(newPath); err == nil {
				return fmt.Errorf("target exists: %s", newPath)
			}
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			return fmt.Errorf("rename file: %w", err)
		}
	}

	if change.TextDocumentEdit != nil {
		edits := make([]protocol.TextEdit, len(change.TextDocumentEdit.Edits))
		for i, e := range change.TextDocumentEdit.Edits {
			textEdit, err := e.AsTextEdit()
			if err != nil {
				return fmt.Errorf("invalid edit type: %w", err)
			}
			edits[i] = textEdit
		}
		if err := applyTextEdits(change.TextDocumentEdit.TextDocument.URI, edits); err != nil {
			return err
		}
	}
	return nil
}

func rangesOverlap(r1, r2 protocol.Range) bool {
	if r1.Start.Line > r2.End.Line || r2.Start.Line > r1.End.Line {
		return false
	}
	if r1.Start.Line == r2.End.Line && r1.Start.Character > r2.End.Character {
		return false
	}
	if r2.Start.Line == r1.End.Line && r2.Start.Character > r1.End.Character {
		return false
	}
	return true
}
