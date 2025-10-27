package highlight

import (
	"bytes"
	"image/color"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	chromaStyles "github.com/alecthomas/chroma/v2/styles"
	"tui/styles"
)

func SyntaxHighlight(source, fileName string, bg color.Color) (string, error) {
	lexer := lexers.Match(fileName)
	if lexer == nil {
		lexer = lexers.Analyse(source)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	formatter := formatters.Get("terminal16m")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	style := chroma.MustNewStyle("opperator", styles.GetChromaTheme())
	builtStyle, err := style.Builder().Transform(func(entry chroma.StyleEntry) chroma.StyleEntry {
		r, g, b, _ := bg.RGBA()
		entry.Background = chroma.NewColour(uint8(r>>8), uint8(g>>8), uint8(b>>8))
		entry.Background = 0
		return entry
	}).Build()
	if err != nil {
		builtStyle = chromaStyles.Fallback
	}

	tokens, err := lexer.Tokenise(nil, source)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, builtStyle, tokens); err != nil {
		return "", err
	}

	return buf.String(), nil
}
