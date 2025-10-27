package sidebar

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
)

// renderWithNewlines renders text with a style while preserving newlines.
// It splits on newlines, renders each line, then joins them back.
func renderWithNewlines(text string, style lipgloss.Style) string {
	if text == "" {
		return ""
	}
	// If no newlines, just render normally
	if !strings.Contains(text, "\n") {
		return style.Render(text)
	}
	// Split by newline, render each line, join back
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = style.Render(line)
	}
	return strings.Join(lines, "\n")
}

// ParseMarkup parses XML-like markup tags and returns a lipgloss-styled string.
// Supported tags:
//   - <c fg="color">text</c> - Set foreground color
//   - <c bg="color">text</c> - Set background color
//   - <c fg="color" bg="color">text</c> - Set both colors
//   - <b>text</b> - Bold text
//   - <i>text</i> - Italic text
//
// Colors can be:
//   - Named colors: red, green, blue, yellow, cyan, magenta, white, black
//   - Hex colors: #RRGGBB or #RGB
//
// Example:
//
//	ParseMarkup(`<b>Status:</b> <c fg="green">Running</c>`)
func ParseMarkup(input string) string {
	return ParseMarkupWithStyle(input, lipgloss.NewStyle())
}

// ParseMarkupWithStyle parses XML-like markup tags and applies a default style to all text.
// This allows setting a default foreground color for untagged content.
func ParseMarkupWithStyle(input string, defaultStyle lipgloss.Style) string {
	if input == "" {
		return ""
	}

	var result strings.Builder
	pos := 0
	length := len(input)

	for pos < length {
		tagStart := strings.IndexByte(input[pos:], '<')
		if tagStart == -1 {
			remaining := input[pos:]
			if remaining != "" {
				result.WriteString(renderWithNewlines(remaining, defaultStyle))
			}
			break
		}

		// Append text before the tag with default style (only if not empty)
		if tagStart > 0 {
			result.WriteString(renderWithNewlines(input[pos:pos+tagStart], defaultStyle))
		}
		pos += tagStart

		tagEnd := strings.IndexByte(input[pos:], '>')
		if tagEnd == -1 {
			// Malformed tag, append as-is with default style
			result.WriteString(renderWithNewlines(input[pos:], defaultStyle))
			break
		}

		fullTag := input[pos : pos+tagEnd+1]
		pos += tagEnd + 1

		if strings.HasPrefix(fullTag, "</") {
			// Closing tags are ignored in this simple implementation
			continue
		}

		tagContent := fullTag[1 : len(fullTag)-1] // Remove < and >
		parts := strings.Fields(tagContent)
		if len(parts) == 0 {
			continue
		}

		tagName := parts[0]
		attrs := parseAttributes(parts[1:])

		closingTag := fmt.Sprintf("</%s>", tagName)
		closePos := strings.Index(input[pos:], closingTag)
		if closePos == -1 {
			closePos = length - pos
		}

		content := input[pos : pos+closePos]
		pos += closePos

		// Skip the closing tag if it exists
		if pos < length && strings.HasPrefix(input[pos:], closingTag) {
			pos += len(closingTag)
		}

		// Apply styling based on tag - this returns already-styled content
		// DO NOT apply default style again
		styledContent := applyStyle(tagName, attrs, content, defaultStyle)
		result.WriteString(styledContent)
	}

	return result.String()
}

// parseAttributes parses tag attributes in the form: attr="value" or attr='value'
func parseAttributes(parts []string) map[string]string {
	attrs := make(map[string]string)

	// Join parts back in case values had spaces (shouldn't in our simple format)
	fullAttr := strings.Join(parts, " ")

	// Simple attribute parser
	pos := 0
	for pos < len(fullAttr) {
		eqPos := strings.IndexByte(fullAttr[pos:], '=')
		if eqPos == -1 {
			break
		}

		attrName := strings.TrimSpace(fullAttr[pos : pos+eqPos])
		pos += eqPos + 1

		if pos >= len(fullAttr) {
			break
		}

		quote := fullAttr[pos]
		if quote != '"' && quote != '\'' {
			break
		}

		pos++ // Skip opening quote
		valueEnd := strings.IndexByte(fullAttr[pos:], quote)
		if valueEnd == -1 {
			break
		}

		attrValue := fullAttr[pos : pos+valueEnd]
		attrs[attrName] = attrValue

		pos += valueEnd + 1 // Skip closing quote

		// Skip any whitespace before next attribute
		for pos < len(fullAttr) && (fullAttr[pos] == ' ' || fullAttr[pos] == '\t') {
			pos++
		}
	}

	return attrs
}

// applyStyle applies styling to content based on tag name and attributes
func applyStyle(tagName string, attrs map[string]string, content string, defaultStyle lipgloss.Style) string {
	switch tagName {
	case "c": // Color tag
		style := lipgloss.NewStyle()
		if fg, ok := attrs["fg"]; ok {
			if color := parseColor(fg); color != "" {
				style = style.Foreground(lipgloss.Color(color))
			}
		}
		if bg, ok := attrs["bg"]; ok {
			if color := parseColor(bg); color != "" {
				style = style.Background(lipgloss.Color(color))
			}
		}
		// Recursively parse content with the color style as the new default
		return ParseMarkupWithStyle(content, style)

	case "b": // Bold
		// Inherit default style and add bold
		style := defaultStyle.Copy().Bold(true)
		// Recursively parse content with bold+default as the new default
		return ParseMarkupWithStyle(content, style)

	case "i": // Italic
		// Inherit default style and add italic
		style := defaultStyle.Copy().Italic(true)
		// Recursively parse content with italic+default as the new default
		return ParseMarkupWithStyle(content, style)

	default:
		// Unknown tag, recursively parse content with default style
		return ParseMarkupWithStyle(content, defaultStyle)
	}
}

// parseColor converts a color name or hex code to a lipgloss color string
func parseColor(color string) string {
	color = strings.TrimSpace(color)
	if color == "" {
		return ""
	}

	if strings.HasPrefix(color, "#") {
		return color
	}

	// Map common color names to hex codes
	colorMap := map[string]string{
		"red":     "#FF0000",
		"green":   "#00FF00",
		"blue":    "#0000FF",
		"yellow":  "#FFFF00",
		"cyan":    "#00FFFF",
		"magenta": "#FF00FF",
		"white":   "#FFFFFF",
		"black":   "#000000",
		"orange":  "#FFA500",
		"purple":  "#800080",
		"pink":    "#FFC0CB",
		"brown":   "#A52A2A",
		"gray":    "#808080",
		"grey":    "#808080",
	}

	if hex, ok := colorMap[strings.ToLower(color)]; ok {
		return hex
	}

	return color
}
