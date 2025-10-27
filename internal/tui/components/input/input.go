package input

import (
	"fmt"
	"image/color"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"tui/commands"
	"tui/components/textarea"
	"tui/coreagent"
	"tui/styles"
)

type Input struct {
	w, h int
	ta   *textarea.Model
	f    bool // focused

	picker commandPicker

	argHint         string
	argHintActive   bool
	argHintStyle    lipgloss.Style
	argHintStyleSet bool

	currentAgentID string // current core agent ID for filtering commands
}

func (c *Input) clearArgHint() {
	c.argHint = ""
	c.argHintActive = false
	c.argHintStyle = lipgloss.Style{}
	c.argHintStyleSet = false
	if c.ta != nil {
		c.ta.ClearGhost()
	}
}

func (c *Input) removeArgHint() {
	if !c.argHintActive {
		c.clearArgHint()
		return
	}
	c.clearArgHint()
}

func (c *Input) applyCommand(cmd commands.Command, keepPicker bool) {
	c.initIfNeeded()
	c.removeArgHint()
	value := strings.TrimSpace(cmd.Name)
	if value == "" {
		if keepPicker {
			c.ensurePickerOpen()
		}
		return
	}
	if cmd.RequiresArgument {
		value = strings.TrimSpace(value) + " "
	}
	c.ta.SetValue(value)
	c.ta.SetCursorColumn(len(value))
	if cmd.RequiresArgument {
		hint := strings.TrimSpace(cmd.ArgumentHint)
		if hint == "" {
			hint = "enter argument..."
		}
		c.argHint = hint
		c.argHintActive = true
		c.argHintStyle = lipgloss.NewStyle().Foreground(colorToLipgloss(styles.CurrentTheme().FgMutedMore))
		c.argHintStyleSet = true
		c.ta.SetGhost(hint, c.argHintStyle)
	}
	if keepPicker {
		c.ensurePickerOpen()
		c.picker.Filter(strings.TrimPrefix(cmd.Name, "/"))
	} else {
		c.picker.Close()
	}
}

func (c *Input) initIfNeeded() {
	if c.ta != nil { // already configured
		return
	}
	ta := textarea.New()
	ta.Placeholder = "Send a message..."
	ta.Focus()
	ta.Prompt = "┃ "
	ta.CharLimit = 100000
	ta.ShowLineNumbers = false

	t := styles.CurrentTheme()
	st := t.S().TextArea
	st.Cursor.Blink = true
	st.Cursor.Shape = tea.CursorBar
	ta.SetStyles(st)

	c.ta = ta
	c.f = true
	c.picker = newCommandPicker()
}

func (c *Input) Init() tea.Cmd {
	c.initIfNeeded()
	return textarea.Blink
}

func (c *Input) Update(msg tea.Msg) tea.Cmd {
	c.initIfNeeded()
	switch m := msg.(type) {
	case tea.KeyMsg:
		if !c.f {
			return nil
		}
		if handled, action := c.picker.HandleKey(m); handled {
			if action.hasCmd {
				c.applyCommand(action.command, action.insert)
			} else if action.close {
				c.picker.Close()
			}
			return nil
		}
		if m.String() == "/" && strings.TrimSpace(c.Value()) == "" {
			c.ensurePickerOpen()
			c.picker.Filter("")
		}
	case tea.KeyPressMsg:
		if !c.f {
			return nil
		}
	}
	var cmd tea.Cmd
	c.ta, cmd = c.ta.Update(msg)
	c.refreshPickerState()
	c.refreshArgHintState()
	return cmd
}

func (c *Input) SetSize(w, h int) {
	c.w, c.h = w, h
	c.initIfNeeded()
	if w > 0 {
		c.ta.SetWidth(w)
	}
	if h > 0 {
		c.ta.SetHeight(h)
	}
	if w > 0 {
		maxWidth := w - c.CommandPickerXOffset()
		if maxWidth <= 0 {
			maxWidth = w
		}
		c.picker.SetMaxWidth(maxWidth)
	}
}

func (c *Input) View() string {
	c.initIfNeeded()
	return c.ta.View()
}

// Public helpers to avoid exposing textarea.Model directly.
func (c *Input) InsertNewline() { c.initIfNeeded(); c.ta.InsertRune('\n') }

func (c *Input) Value() string {
	c.initIfNeeded()
	return c.ta.Value()
}

func (c *Input) SetValue(v string) {
	c.initIfNeeded()
	c.clearArgHint()
	c.ta.SetValue(v)
	c.refreshPickerState()
}

func (c *Input) Focus() tea.Cmd {
	c.initIfNeeded()
	c.f = true
	return c.ta.Focus()
}

func (c *Input) Blur() tea.Cmd {
	c.initIfNeeded()
	c.f = false
	c.ta.Blur()
	c.picker.Close()
	return nil
}

func (c *Input) IsFocused() bool { c.initIfNeeded(); return c.f }

func (c *Input) SetCurrentAgentID(agentID string) {
	c.currentAgentID = agentID
}

func (c *Input) ensurePickerOpen() {
	maxWidth := c.w - c.CommandPickerXOffset()
	if maxWidth <= 0 {
		maxWidth = c.w
	}
	if !c.picker.IsOpen() {
		// Filter commands based on current agent
		allCommands := commands.List()
		filteredCommands := make([]commands.Command, 0, len(allCommands))
		for _, cmd := range allCommands {
			// Exclude /focus command unless in Builder mode
			if cmd.Name == "/focus" && c.currentAgentID != coreagent.IDBuilder {
				continue
			}
			filteredCommands = append(filteredCommands, cmd)
		}
		c.picker.Open(filteredCommands, maxWidth)
		return
	}
	c.picker.SetMaxWidth(maxWidth)
}

func (c *Input) refreshPickerState() {
	if c.ta == nil {
		return
	}
	value := c.ta.Value()
	if value == "" {
		c.picker.Close()
		return
	}
	if strings.ContainsAny(value, " \n\t") || !strings.HasPrefix(value, "/") {
		c.picker.Close()
		return
	}
	c.ensurePickerOpen()
	c.picker.Filter(strings.TrimPrefix(value, "/"))
}

func (c *Input) refreshArgHintState() {
	if c.ta == nil {
		return
	}

	cmd, rest, ok := matchCommandInput(c.ta.Value())
	if !ok || !cmd.RequiresArgument {
		if c.argHintActive {
			c.clearArgHint()
		}
		return
	}

	if rest == "" || rest[0] != ' ' {
		if c.argHintActive {
			c.clearArgHint()
		}
		return
	}

	if strings.TrimSpace(rest) != "" {
		if c.argHintActive {
			c.clearArgHint()
		}
		return
	}

	hint := strings.TrimSpace(cmd.ArgumentHint)
	if hint == "" {
		hint = "enter argument..."
	}

	style := c.argHintStyle
	if !c.argHintActive || c.argHint != hint || !c.argHintStyleSet {
		style = lipgloss.NewStyle().Foreground(colorToLipgloss(styles.CurrentTheme().FgMutedMore))
		c.argHint = hint
		c.argHintStyle = style
		c.argHintActive = true
		c.argHintStyleSet = true
	}

	c.ta.SetGhost(c.argHint, c.argHintStyle)
}

func (c *Input) CommandPickerView() string {
	if !c.picker.IsActive() {
		return ""
	}
	return c.picker.View()
}

func (c *Input) CommandPickerHeight() int {
	return c.picker.Height()
}

func (c *Input) CommandPickerWidth() int {
	return c.picker.Width()
}

func (c *Input) CommandPickerXOffset() int {
	c.initIfNeeded()
	return lipgloss.Width(c.ta.Prompt)
}

func (c *Input) CommandPickerIsOpen() bool {
	return c.picker.IsActive()
}

func (c *Input) SelectCommand() (commands.Command, bool) {
	if !c.picker.IsActive() {
		return commands.Command{}, false
	}
	cmd, ok := c.picker.Selected()
	if !ok {
		return commands.Command{}, false
	}
	c.applyCommand(cmd, false)
	return cmd, true
}

func colorToLipgloss(c color.Color) color.Color {
	if rgba, ok := c.(color.RGBA); ok {
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", rgba.R, rgba.G, rgba.B))
	}
	r, g, b, _ := c.RGBA()
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8)))
}

func matchCommandInput(value string) (commands.Command, string, bool) {
	trimmedLeading := strings.TrimLeftFunc(value, unicode.IsSpace)
	if trimmedLeading == "" || !strings.HasPrefix(trimmedLeading, "/") {
		return commands.Command{}, "", false
	}

	for _, cmd := range commands.List() {
		if !strings.HasPrefix(trimmedLeading, cmd.Name) {
			continue
		}

		if len(trimmedLeading) > len(cmd.Name) {
			delim := trimmedLeading[len(cmd.Name)]
			if !unicode.IsSpace(rune(delim)) {
				continue
			}
		}

		rest := ""
		if len(trimmedLeading) > len(cmd.Name) {
			rest = trimmedLeading[len(cmd.Name):]
		}
		return cmd, rest, true
	}

	return commands.Command{}, "", false
}
