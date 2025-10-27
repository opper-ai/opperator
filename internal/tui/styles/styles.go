package styles

import (
	"fmt"
	"image/color"

	"github.com/charmbracelet/bubbles/v2/help"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/glamour/v2/ansi"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
	"tui/components/textarea"
)

const (
	defaultListIndent uint = 2
	defaultMargin     uint = 2
)

func boolPtr(b bool) *bool       { return &b }
func stringPtr(s string) *string { return &s }
func uintPtr(u uint) *uint       { return &u }

// Theme models a richer palette inspired by the reference.
type Theme struct {
	Name   string
	IsDark bool

	Primary   color.Color
	Secondary color.Color
	Accent    color.Color

	BgBase        color.Color
	BgBaseLighter color.Color
	BgSubtle      color.Color
	BgOverlay     color.Color

	FgBase      color.Color
	FgMuted     color.Color
	FgMutedMore color.Color
	FgSubtle    color.Color
	FgSelected  color.Color

	Border      color.Color
	BorderFocus color.Color

	Success color.Color
	Error   color.Color
	Warning color.Color
	Info    color.Color

	Red    color.Color
	Green  color.Color
	Yellow color.Color

	styles *Styles
	// Common
	White color.Color
}

// Styles are common pre-built lipgloss styles.
type Styles struct {
	Base         lipgloss.Style
	SelectedBase lipgloss.Style

	Title    lipgloss.Style
	Subtitle lipgloss.Style
	Text     lipgloss.Style
	Muted    lipgloss.Style
	Subtle   lipgloss.Style

	// Markdown & Chroma
	Markdown ansi.StyleConfig

	TextArea textarea.Styles
	Help     help.Styles
}

// S returns lazily-initialized styles tied to the theme colors.
func (t *Theme) S() *Styles {
	if t.styles != nil {
		return t.styles
	}
	base := lipgloss.NewStyle().Foreground(t.FgBase)
	s := &Styles{
		Base:         base,
		SelectedBase: base.Background(t.Primary).Foreground(t.FgSelected),
		Title:        base.Foreground(t.Primary).Bold(true),
		Subtitle:     base.Foreground(t.Secondary).Bold(true),
		Text:         base,
		Muted:        base.Foreground(t.FgMuted),
		Subtle:       base.Foreground(t.FgSubtle),
	}

	s.TextArea = textarea.Styles{
		Focused: textarea.StyleState{
			Base:             base,
			Text:             base,
			LineNumber:       base.Foreground(t.FgSubtle),
			CursorLine:       base,
			CursorLineNumber: base.Foreground(t.FgSubtle),
			Placeholder:      base.Foreground(t.FgSubtle),
			Prompt:           base.Foreground(t.Primary),
		},
		Blurred: textarea.StyleState{
			Base:             base,
			Text:             base.Foreground(t.FgMuted),
			LineNumber:       base.Foreground(t.FgMuted),
			CursorLine:       base,
			CursorLineNumber: base.Foreground(t.FgMuted),
			Placeholder:      base.Foreground(t.FgSubtle),
			Prompt:           base.Foreground(t.FgMuted),
		},
		Cursor: textarea.CursorStyle{
			Color: t.Primary,
			Shape: tea.CursorUnderline,
			Blink: true,
		},
	}

	s.Markdown = ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				// BlockPrefix: "\n",
				// BlockSuffix: "\n",
				Color: stringPtr(charmtone.Smoke.Hex()),
			},
			// Margin: uintPtr(defaultMargin),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{},
			Indent:         uintPtr(1),
			IndentToken:    stringPtr("│ "),
		},
		List: ansi.StyleList{
			LevelIndent: defaultListIndent,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       stringPtr(charmtone.Malibu.Hex()),
				Bold:        boolPtr(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          "",
				Suffix:          "",
				Color:           stringPtr(charmtone.Zest.Hex()),
				BackgroundColor: stringPtr(charmtone.Charple.Hex()),
				Bold:            boolPtr(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "",
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "",
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "",
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "",
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "",
				Color:  stringPtr(charmtone.Guac.Hex()),
				Bold:   boolPtr(false),
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: boolPtr(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: boolPtr(true),
		},
		Strong: ansi.StylePrimitive{
			Bold: boolPtr(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  stringPtr(charmtone.Charcoal.Hex()),
			Format: "\n--------\n",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			StylePrimitive: ansi.StylePrimitive{},
			Ticked:         "[✓] ",
			Unticked:       "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     stringPtr(charmtone.Zinc.Hex()),
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: stringPtr(charmtone.Guac.Hex()),
			Bold:  boolPtr(true),
		},
		Image: ansi.StylePrimitive{
			Color:     stringPtr(charmtone.Cheeky.Hex()),
			Underline: boolPtr(true),
		},
		ImageText: ansi.StylePrimitive{
			Color:  stringPtr(charmtone.Squid.Hex()),
			Format: "Image: {{.text}} →",
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           stringPtr("#f7c0af"),
				BackgroundColor: stringPtr("#2a2a2e"),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Charcoal.Hex()),
				},
				Margin: uintPtr(defaultMargin),
			},
			Chroma: &ansi.Chroma{
				Text: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Smoke.Hex()),
				},
				Error: ansi.StylePrimitive{
					Color:           stringPtr(charmtone.Butter.Hex()),
					BackgroundColor: stringPtr(charmtone.Sriracha.Hex()),
				},
				Comment: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Oyster.Hex()),
				},
				CommentPreproc: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Bengal.Hex()),
				},
				Keyword: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Malibu.Hex()),
				},
				KeywordReserved: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Pony.Hex()),
				},
				KeywordNamespace: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Pony.Hex()),
				},
				KeywordType: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Guppy.Hex()),
				},
				Operator: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Salmon.Hex()),
				},
				Punctuation: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Zest.Hex()),
				},
				Name: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Smoke.Hex()),
				},
				NameBuiltin: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Cheeky.Hex()),
				},
				NameTag: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Mauve.Hex()),
				},
				NameAttribute: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Hazy.Hex()),
				},
				NameClass: ansi.StylePrimitive{
					Color:     stringPtr(charmtone.Salt.Hex()),
					Underline: boolPtr(true),
					Bold:      boolPtr(true),
				},
				NameDecorator: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Citron.Hex()),
				},
				NameFunction: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Guac.Hex()),
				},
				LiteralNumber: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Julep.Hex()),
				},
				LiteralString: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Cumin.Hex()),
				},
				LiteralStringEscape: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Bok.Hex()),
				},
				GenericDeleted: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Coral.Hex()),
				},
				GenericEmph: ansi.StylePrimitive{
					Italic: boolPtr(true),
				},
				GenericInserted: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Guac.Hex()),
				},
				GenericStrong: ansi.StylePrimitive{
					Bold: boolPtr(true),
				},
				GenericSubheading: ansi.StylePrimitive{
					Color: stringPtr(charmtone.Squid.Hex()),
				},
				Background: ansi.StylePrimitive{
					BackgroundColor: stringPtr(charmtone.Charcoal.Hex()),
				},
			},
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
			},
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "\n ",
		},
	}

	s.Help = help.Styles{
		Ellipsis:       base.Foreground(t.FgMuted).SetString("…"),
		ShortKey:       base.Foreground(t.FgMuted),
		ShortDesc:      base.Foreground(t.FgMutedMore),
		ShortSeparator: base.Foreground(t.FgMuted).SetString(""),
		FullKey:        base.Foreground(t.FgMuted).Bold(true),
		FullDesc:       base.Foreground(t.FgBase),
		FullSeparator:  base.Foreground(t.FgSubtle).SetString("\n"),
	}

	t.styles = s
	return s
}

func CurrentTheme() Theme {
	// Core colors
	bg := color.RGBA{0x10, 0x10, 0x12, 0xff}
	fg := color.RGBA{0xdd, 0xdd, 0xdd, 0xff}
	primary := lipgloss.Color("#f7c0af")   // orangish
	secondary := lipgloss.Color("#3ccad7") // cyan

	// Supporting tones
	fgMuted := color.RGBA{0x7f, 0x7f, 0x7f, 0xff}
	fgMutedMore := color.RGBA{0x58, 0x58, 0x58, 0x58}
	fgSubtle := color.RGBA{0x88, 0x88, 0x88, 0xff}
	border := color.RGBA{0x33, 0x33, 0x38, 0xff}
	borderFocus := primary
	accent := secondary

	return Theme{
		Name:   "Dark",
		IsDark: true,

		Primary:   primary,
		Secondary: secondary,
		Accent:    accent,

		BgBase:        bg,
		BgBaseLighter: lipgloss.Color("#3ccad7"),
		BgSubtle:      color.RGBA{0x12, 0x12, 0x14, 0xff},
		BgOverlay:     color.RGBA{0x0c, 0x0c, 0x0f, 0x99},

		FgBase:      fg,
		FgMuted:     fgMuted,
		FgMutedMore: fgMutedMore,
		FgSubtle:    fgSubtle,
		FgSelected:  color.RGBA{0x0b, 0x0b, 0x0d, 0xff},

		Border:      border,
		BorderFocus: borderFocus,

		Success: color.RGBA{0x87, 0xbf, 0x47, 0xff}, // green
		Error:   color.RGBA{0xbf, 0x5d, 0x47, 0xff}, // red
		Warning: color.RGBA{0xff, 0xc1, 0x07, 0xff}, // yellow
		Info:    color.RGBA{0x64, 0xb5, 0xf6, 0xff}, // blue

		Red:    color.RGBA{0xbf, 0x5d, 0x47, 0xff},
		Green:  color.RGBA{0x87, 0xbf, 0x47, 0xff},
		Yellow: color.RGBA{0xff, 0xc1, 0x07, 0xff},

		White: color.RGBA{0xff, 0xff, 0xff, 0xff},
	}
}

// ApplyBoldForegroundGrad applies a simple foreground gradient across text.
// Falls back to solid color if the terminal doesn't support TrueColor.
func ApplyBoldForegroundGrad(text string, from, to color.Color) string {
	rs := []rune(text)
	n := len(rs)
	if n == 0 {
		return ""
	}

	// Check if terminal supports TrueColor
	profile := termenv.ColorProfile()
	if profile != termenv.TrueColor {
		// Fallback to solid color using 'from' color
		c1, _ := colorful.MakeColor(from)
		hex := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", uint8(c1.R*255), uint8(c1.G*255), uint8(c1.B*255)))
		return lipgloss.NewStyle().Foreground(hex).Bold(true).Render(text)
	}

	// Apply gradient for TrueColor terminals
	c1, _ := colorful.MakeColor(from)
	c2, _ := colorful.MakeColor(to)
	var out string
	for i, r := range rs {
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		c := c1.BlendLab(c2, t)
		hex := lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", uint8(c.R*255), uint8(c.G*255), uint8(c.B*255)))
		out += lipgloss.NewStyle().Foreground(hex).Bold(true).Render(string(r))
	}
	return out
}
