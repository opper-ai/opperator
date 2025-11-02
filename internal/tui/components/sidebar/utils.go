package sidebar

import "strings"

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// truncateToOneLine truncates a string to a single line and a maximum width
func truncateToOneLine(text string, maxWidth int) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\t", " ")

	for strings.Contains(text, "  ") {
		text = strings.ReplaceAll(text, "  ", " ")
	}

	text = strings.TrimSpace(text)

	if maxWidth > 3 && len(text) > maxWidth {
		return text[:maxWidth-3] + "..."
	}

	return text
}

// stripMarkupTags removes XML-like markup tags from text
func stripMarkupTags(text string) string {
	var result strings.Builder
	inTag := false

	for _, ch := range text {
		if ch == '<' {
			inTag = true
			continue
		}
		if ch == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(ch)
		}
	}

	return result.String()
}

// stringSlicesEqual compares two string slices for equality
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// agentListEqual compares two AgentListItem slices for equality
func agentListEqual(a, b []AgentListItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name ||
			a[i].Description != b[i].Description ||
			a[i].Status != b[i].Status ||
			a[i].Color != b[i].Color ||
			a[i].Daemon != b[i].Daemon {
			return false
		}
	}
	return true
}

// customSectionsEqual compares two CustomSection slices for equality
func customSectionsEqual(a, b []CustomSection) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID ||
			a[i].Title != b[i].Title ||
			a[i].Content != b[i].Content ||
			a[i].Collapsed != b[i].Collapsed {
			return false
		}
	}
	return true
}

// todosEqual compares two TodoItem slices for equality
func todosEqual(a, b []TodoItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID ||
			a[i].Text != b[i].Text ||
			a[i].Completed != b[i].Completed {
			return false
		}
	}
	return true
}
