package input

import (
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"tui/commands"
	"tui/styles"
)

const (
	maxCommandPickerHeight = 10
	minCommandPickerWidth  = 16
)

type pickerKeyMap struct {
	Down       key.Binding
	Up         key.Binding
	Select     key.Binding
	Cancel     key.Binding
	DownInsert key.Binding
	UpInsert   key.Binding
}

func newPickerKeyMap() pickerKeyMap {
	return pickerKeyMap{
		Down: key.NewBinding(
			key.WithKeys("down", "ctrl+j"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "ctrl+k"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
		),
		DownInsert: key.NewBinding(
			key.WithKeys("ctrl+n"),
		),
		UpInsert: key.NewBinding(
			key.WithKeys("ctrl+p"),
		),
	}
}

type pickerItem struct {
	command commands.Command
	matches []int
}

type commandPicker struct {
	open     bool
	query    string
	all      []commands.Command
	filtered []pickerItem
	index    int
	width    int
	height   int
	maxWidth int
	keyMap   pickerKeyMap
}

func newCommandPicker() commandPicker {
	return commandPicker{
		keyMap: newPickerKeyMap(),
		width:  minCommandPickerWidth,
	}
}

func (c *commandPicker) Open(cmds []commands.Command, width int) {
	c.all = append([]commands.Command(nil), cmds...)
	c.maxWidth = width
	c.query = ""
	c.index = 0
	c.open = true

	c.filtered = make([]pickerItem, len(c.all))
	for i, cmd := range c.all {
		c.filtered[i] = pickerItem{command: cmd}
	}
	c.recalculate()
}

func (c *commandPicker) Close() {
	c.open = false
	c.query = ""
	c.filtered = nil
	c.index = 0
	c.width = minCommandPickerWidth
	c.height = 0
}

func (c *commandPicker) IsOpen() bool {
	return c.open
}

func (c *commandPicker) IsActive() bool {
	return c.open && len(c.filtered) > 0
}

func (c *commandPicker) Height() int {
	if !c.IsActive() {
		return 0
	}
	return c.height
}

func (c *commandPicker) Width() int {
	if !c.IsActive() {
		return 0
	}
	return c.width
}

func (c *commandPicker) SetMaxWidth(w int) {
	if c.maxWidth == w {
		return
	}
	c.maxWidth = w
	if c.open {
		c.recalculate()
	}
}

func (c *commandPicker) Move(delta int) {
	if !c.open || len(c.filtered) == 0 {
		return
	}
	c.index += delta
	if c.index < 0 {
		c.index = 0
	}
	if c.index >= len(c.filtered) {
		c.index = len(c.filtered) - 1
	}
}

func (c *commandPicker) Selected() (commands.Command, bool) {
	if !c.open || len(c.filtered) == 0 {
		return commands.Command{}, false
	}
	if c.index < 0 || c.index >= len(c.filtered) {
		return commands.Command{}, false
	}
	return c.filtered[c.index].command, true
}

func (c *commandPicker) Filter(query string) {
	if !c.open {
		return
	}
	if query == c.query && len(c.filtered) > 0 {
		return
	}
	c.query = query

	if query == "" {
		c.filtered = make([]pickerItem, len(c.all))
		for i, cmd := range c.all {
			c.filtered[i] = pickerItem{command: cmd}
		}
		c.index = 0
		c.recalculate()
		return
	}

	haystack := make([]string, len(c.all))
	for i, cmd := range c.all {
		haystack[i] = strings.ToLower(cmd.Name)
	}

	matches := fuzzyFind(strings.ToLower(query), haystack)
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	filtered := make([]pickerItem, 0, len(matches))
	for _, match := range matches {
		filtered = append(filtered, pickerItem{
			command: c.all[match.Index],
			matches: match.MatchedIndexes,
		})
	}

	c.filtered = filtered
	if len(c.filtered) == 0 {
		c.index = 0
	} else if c.index >= len(c.filtered) {
		c.index = len(c.filtered) - 1
	}
	c.recalculate()
}

type pickerAction struct {
	command commands.Command
	hasCmd  bool
	insert  bool
	close   bool
}

func (c *commandPicker) HandleKey(msg tea.KeyMsg) (bool, pickerAction) {
	if !c.open {
		return false, pickerAction{}
	}
	switch {
	case key.Matches(msg, c.keyMap.Up):
		c.Move(-1)
		return true, pickerAction{}
	case key.Matches(msg, c.keyMap.Down):
		c.Move(1)
		return true, pickerAction{}
	case key.Matches(msg, c.keyMap.UpInsert), key.Matches(msg, c.keyMap.DownInsert):
		if cmd, ok := c.Selected(); ok {
			return true, pickerAction{command: cmd, hasCmd: true, insert: true}
		}
		return true, pickerAction{}
	case key.Matches(msg, c.keyMap.Select):
		if cmd, ok := c.Selected(); ok {
			c.open = false
			return true, pickerAction{command: cmd, hasCmd: true}
		}
		c.open = false
		return true, pickerAction{}
	case key.Matches(msg, c.keyMap.Cancel):
		c.open = false
		return true, pickerAction{close: true}
	}
	return false, pickerAction{}
}

func (c *commandPicker) View() string {
	if !c.open || len(c.filtered) == 0 {
		return ""
	}

	t := styles.CurrentTheme()
	baseLine := t.S().Base.Background(t.BgSubtle).Padding(0, 1).Width(c.width)
	selectedLine := t.S().SelectedBase.Padding(0, 1).Width(c.width)

	contentWidth := c.width
	if contentWidth >= 2 {
		contentWidth -= 2
	}

	start := 0
	if c.height > 0 && c.index >= c.height {
		start = c.index - c.height + 1
	}
	end := len(c.filtered)
	if c.height > 0 && start+c.height < end {
		end = start + c.height
	}

	var lines []string
	for i := start; i < end; i++ {
		item := c.filtered[i]
		rowStyle := baseLine
		if i == c.index {
			rowStyle = selectedLine
		}

		name := renderMatches(item.command.Name, item.matches, i == c.index)
		line := name
		if desc := strings.TrimSpace(item.command.Description); desc != "" {
			if i == c.index {
				line += t.S().SelectedBase.Render("  " + desc)
			} else {
				line += t.S().Base.Render("  " + desc)
			}
		}

		if contentWidth > 0 {
			line = ansi.Truncate(line, contentWidth, "…")
		}

		lines = append(lines, rowStyle.Render(line))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func renderMatches(text string, matches []int, selected bool) string {
	if len(matches) == 0 {
		return text
	}
	matchSet := make(map[int]struct{}, len(matches))
	for _, idx := range matches {
		matchSet[idx] = struct{}{}
	}
	t := styles.CurrentTheme()

	var sb strings.Builder
	for i, r := range text {
		if _, ok := matchSet[i]; ok {
			if selected {
				sb.WriteString(t.S().SelectedBase.Bold(true).Render(string(r)))
			} else {
				sb.WriteString(t.S().Base.Bold(true).Render(string(r)))
			}
		} else {
			if selected {
				sb.WriteString(t.S().SelectedBase.Render(string(r)))
			} else {
				sb.WriteString(t.S().Base.Render(string(r)))
			}
		}
	}
	return sb.String()
}

type fuzzyMatch struct {
	Index          int
	Score          int
	MatchedIndexes []int
}

func fuzzyFind(query string, words []string) []fuzzyMatch {
	if query == "" {
		return nil
	}
	var matches []fuzzyMatch
	lowerRunes := []rune(query)
	for idx, word := range words {
		positions := matchPositions(lowerRunes, word)
		if positions == nil {
			continue
		}
		score := scoreMatch(positions)
		matches = append(matches, fuzzyMatch{Index: idx, Score: score, MatchedIndexes: positions})
	}
	return matches
}

func matchPositions(query []rune, word string) []int {
	if len(query) == 0 {
		return nil
	}
	positions := make([]int, 0, len(query))
	qi := 0
	for idx, r := range word {
		if qi >= len(query) {
			break
		}
		if unicode.ToLower(r) == query[qi] {
			positions = append(positions, idx)
			qi++
		}
	}
	if qi != len(query) {
		return nil
	}
	return positions
}

func scoreMatch(positions []int) int {
	if len(positions) == 0 {
		return 0
	}
	spread := positions[len(positions)-1] - positions[0]
	if spread < 0 {
		spread = 0
	}
	score := 100 - spread + len(positions)*5
	if positions[0] == 0 {
		score += 20
	}
	return score
}

func (c *commandPicker) recalculate() {
	c.height = len(c.filtered)
	if c.height > maxCommandPickerHeight {
		c.height = maxCommandPickerHeight
	}
	if c.height < 0 {
		c.height = 0
	}

	width := minCommandPickerWidth
	for _, item := range c.filtered {
		lineWidth := lipgloss.Width(item.command.Name)
		if desc := strings.TrimSpace(item.command.Description); desc != "" {
			lineWidth += 2 + lipgloss.Width(desc)
		}
		lineWidth += 2 // padding
		if lineWidth > width {
			width = lineWidth
		}
	}
	if c.maxWidth > 0 && width > c.maxWidth {
		width = c.maxWidth
	}
	c.width = width
}
