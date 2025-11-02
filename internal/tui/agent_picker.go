package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tui/coreagent"
	llm "tui/llm"
	"tui/styles"
	tooling "tui/tools"
	"tui/util"
)

const (
	maxAgentPickerHeight = 10
	minAgentPickerWidth  = 16
)

type agentPickerOption struct {
	name   string
	label  string
	status string
	daemon string // Which daemon this agent is on
}

type agentPickerItem struct {
	option  agentPickerOption
	matches []int
}

type agentPicker struct {
	options  []agentPickerOption
	filtered []agentPickerItem
	query    string
	index    int
	width    int
	height   int
	maxWidth int
}

func newFocusAgentPicker(agents []llm.AgentInfo, maxWidth int, focusedAgent string) *agentPicker {
	opts := []agentPickerOption{
		{name: "", label: "no agent", status: "unfocus"},
	}

	seen := make(map[string]struct{})

	sort.Slice(agents, func(i, j int) bool {
		ai := strings.ToLower(strings.TrimSpace(agents[i].Name))
		aj := strings.ToLower(strings.TrimSpace(agents[j].Name))
		if ai == aj {
			return strings.ToLower(strings.TrimSpace(agents[i].Status)) < strings.ToLower(strings.TrimSpace(agents[j].Status))
		}
		return ai < aj
	})
	for _, agent := range agents {
		trimmed := strings.TrimSpace(agent.Name)
		if trimmed == "" {
			continue
		}
		key := "remote:" + strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		opts = append(opts, agentPickerOption{
			name:   trimmed,
			label:  trimmed,
			status: strings.ToLower(strings.TrimSpace(agent.Status)),
			daemon: strings.TrimSpace(agent.Daemon),
		})
	}

	picker := &agentPicker{options: opts, maxWidth: maxWidth}
	picker.Filter("")
	if strings.TrimSpace(focusedAgent) != "" {
		picker.selectByName(focusedAgent)
	}
	return picker
}

func newAgentPicker(agents []llm.AgentInfo, builtins []coreagent.Definition, maxWidth int, activeRemote, activeCore string) *agentPicker {
	opts := make([]agentPickerOption, 0, len(agents)+len(builtins))

	seen := make(map[string]struct{})

	for _, def := range builtins {
		id := strings.TrimSpace(def.ID)
		if id == "" {
			continue
		}
		key := "core:" + strings.ToLower(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		label := strings.TrimSpace(def.Name)
		opts = append(opts, agentPickerOption{name: id, label: label, status: "core"})
	}

	sort.Slice(agents, func(i, j int) bool {
		ai := strings.ToLower(strings.TrimSpace(agents[i].Name))
		aj := strings.ToLower(strings.TrimSpace(agents[j].Name))
		if ai == aj {
			return strings.ToLower(strings.TrimSpace(agents[i].Status)) < strings.ToLower(strings.TrimSpace(agents[j].Status))
		}
		return ai < aj
	})
	for _, agent := range agents {
		trimmed := strings.TrimSpace(agent.Name)
		if trimmed == "" {
			continue
		}
		key := "remote:" + strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		opts = append(opts, agentPickerOption{
			name:   trimmed,
			label:  trimmed,
			status: strings.ToLower(strings.TrimSpace(agent.Status)),
			daemon: strings.TrimSpace(agent.Daemon),
		})
	}

	picker := &agentPicker{options: opts, maxWidth: maxWidth}
	picker.Filter("")
	if strings.TrimSpace(activeRemote) != "" {
		picker.selectByName(activeRemote)
	} else if strings.TrimSpace(activeCore) != "" {
		picker.selectByName(activeCore)
	}
	return picker
}

func (p *agentPicker) Filter(query string) {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if p.filtered != nil && p.query == normalized {
		return
	}
	p.query = normalized

	if normalized == "" {
		p.filtered = make([]agentPickerItem, len(p.options))
		for i, opt := range p.options {
			p.filtered[i] = agentPickerItem{option: opt}
		}
	} else {
		haystack := make([]string, len(p.options))
		for i, opt := range p.options {
			candidate := strings.TrimSpace(opt.label)
			if candidate == "" {
				candidate = strings.TrimSpace(opt.name)
			}
			if candidate == "" {
				candidate = "agent"
			}
			haystack[i] = strings.ToLower(candidate)
		}

		matches := fuzzyFind(normalized, haystack)
		sort.SliceStable(matches, func(i, j int) bool {
			if matches[i].Score == matches[j].Score {
				return matches[i].Index < matches[j].Index
			}
			return matches[i].Score > matches[j].Score
		})

		filtered := make([]agentPickerItem, 0, len(matches))
		for _, match := range matches {
			filtered = append(filtered, agentPickerItem{
				option:  p.options[match.Index],
				matches: match.MatchedIndexes,
			})
		}
		p.filtered = filtered
	}

	if len(p.filtered) == 0 {
		p.index = 0
	} else if p.index >= len(p.filtered) {
		p.index = len(p.filtered) - 1
	}

	p.recalculate()
}

func (p *agentPicker) recalculate() {
	// Find the longest agent name for alignment
	maxNameWidth := 0
	maxStatusWidth := 0
	for _, item := range p.filtered {
		name := item.option.label
		if name == "" {
			name = item.option.name
		}
		if strings.TrimSpace(name) == "" {
			name = "Unnamed agent"
		}
		nameWidth := lipgloss.Width(name)
		if nameWidth > maxNameWidth {
			maxNameWidth = nameWidth
		}

		if desc := strings.TrimSpace(item.option.status); desc != "" {
			statusWidth := lipgloss.Width(desc)
			if statusWidth > maxStatusWidth {
				maxStatusWidth = statusWidth
			}
		}
	}

	// Calculate width: name + spacing + status + padding
	// Add extra padding (4 spaces) between name and status for better alignment
	width := maxNameWidth + 4 + maxStatusWidth + 2 // +2 for row padding
	if width < minAgentPickerWidth {
		width = minAgentPickerWidth
	}
	if p.maxWidth > 0 && width > p.maxWidth {
		width = p.maxWidth
	}
	p.width = width

	height := len(p.filtered)
	if height > maxAgentPickerHeight {
		height = maxAgentPickerHeight
	}
	if height < 0 {
		height = 0
	}
	p.height = height
}

func (p *agentPicker) View() string {
	theme := styles.CurrentTheme()
	rowStyle := theme.S().Base.Background(theme.BgSubtle).Padding(0, 1).Width(p.width)
	selectedStyle := theme.S().SelectedBase.Padding(0, 1).Width(p.width)

	if len(p.filtered) == 0 {
		message := theme.S().Muted.Render("No agents found")
		return rowStyle.Render(message)
	}

	contentWidth := p.width
	if contentWidth >= 2 {
		contentWidth -= 2
	}

	// Calculate max name width for alignment
	maxNameWidth := 0
	for _, item := range p.filtered {
		name := item.option.label
		if name == "" {
			name = item.option.name
		}
		if strings.TrimSpace(name) == "" {
			name = "Unnamed agent"
		}
		nameWidth := lipgloss.Width(name)
		if nameWidth > maxNameWidth {
			maxNameWidth = nameWidth
		}
	}

	start := 0
	if p.height > 0 && p.index >= p.height {
		start = p.index - p.height + 1
	}
	end := len(p.filtered)
	if p.height > 0 && start+p.height < end {
		end = start + p.height
	}

	var lines []string
	for i := start; i < end; i++ {
		item := p.filtered[i]
		row := rowStyle
		if i == p.index {
			row = selectedStyle
		}

		name := item.option.label
		if name == "" {
			name = item.option.name
		}
		if strings.TrimSpace(name) == "" {
			name = "Unnamed agent"
		}
		rendered := renderAgentMatches(name, item.matches, i == p.index)

		// Add padding to align status
		nameWidth := lipgloss.Width(name)
		padding := maxNameWidth - nameWidth
		if padding > 0 {
			rendered += strings.Repeat(" ", padding)
		}

		// Add daemon tag if present (for remote agents)
		if daemon := strings.TrimSpace(item.option.daemon); daemon != "" && daemon != "local" {
			var daemonStyle lipgloss.Style
			if i == p.index {
				daemonStyle = theme.S().SelectedBase.Foreground(theme.FgSubtle)
			} else {
				daemonStyle = lipgloss.NewStyle().Foreground(theme.FgSubtle)
			}
			rendered += daemonStyle.Render("  [" + daemon + "]")
		}

		if desc := strings.TrimSpace(item.option.status); desc != "" && item.option.name != "" {
			var descStyle lipgloss.Style
			// Color code the status based on its value
			switch desc {
			case "running":
				if i == p.index {
					descStyle = theme.S().SelectedBase.Foreground(theme.Success)
				} else {
					descStyle = lipgloss.NewStyle().Foreground(theme.Success)
				}
			case "crashed":
				if i == p.index {
					descStyle = theme.S().SelectedBase.Foreground(theme.Error)
				} else {
					descStyle = lipgloss.NewStyle().Foreground(theme.Error)
				}
			case "stopped":
				if i == p.index {
					descStyle = theme.S().SelectedBase.Foreground(theme.FgMuted)
				} else {
					descStyle = lipgloss.NewStyle().Foreground(theme.FgMuted)
				}
			case "core":
				if i == p.index {
					descStyle = theme.S().SelectedBase.Foreground(theme.Accent)
				} else {
					descStyle = lipgloss.NewStyle().Foreground(theme.Accent)
				}
			case "unfocus":
				if i == p.index {
					descStyle = theme.S().SelectedBase.Foreground(theme.FgMuted)
				} else {
					descStyle = lipgloss.NewStyle().Foreground(theme.FgMuted)
				}
			default:
				if i == p.index {
					descStyle = theme.S().SelectedBase.Foreground(theme.FgSubtle)
				} else {
					descStyle = lipgloss.NewStyle().Foreground(theme.FgSubtle)
				}
			}
			rendered += descStyle.Render("  " + desc)
		}

		if contentWidth > 0 {
			rendered = ansi.Truncate(rendered, contentWidth, "…")
		}
		lines = append(lines, row.Render(rendered))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (p *agentPicker) move(delta int) {
	if len(p.filtered) == 0 {
		return
	}
	p.index += delta
	if p.index < 0 {
		p.index = 0
	}
	if p.index >= len(p.filtered) {
		p.index = len(p.filtered) - 1
	}
}

func (p *agentPicker) selectByName(name string) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		p.index = 0
		return
	}
	for i, item := range p.filtered {
		if strings.EqualFold(item.option.name, trimmed) {
			p.index = i
			return
		}
	}
}

func (p *agentPicker) selected() (agentPickerOption, bool) {
	if len(p.filtered) == 0 {
		return agentPickerOption{}, false
	}
	if p.index < 0 || p.index >= len(p.filtered) {
		return agentPickerOption{}, false
	}
	return p.filtered[p.index].option, true
}

func (p *agentPicker) Width() int { return p.width }

func (p *agentPicker) Height() int { return p.height }

func (p *agentPicker) SetMaxWidth(maxWidth int) {
	if maxWidth <= 0 {
		maxWidth = minAgentPickerWidth
	}
	p.maxWidth = maxWidth
	p.recalculate()
}

func renderAgentMatches(text string, matches []int, selected bool) string {
	if len(matches) == 0 {
		return text
	}
	matchSet := make(map[int]struct{}, len(matches))
	for _, idx := range matches {
		matchSet[idx] = struct{}{}
	}
	theme := styles.CurrentTheme()

	var sb strings.Builder
	for i, r := range text {
		if _, ok := matchSet[i]; ok {
			if selected {
				sb.WriteString(theme.S().SelectedBase.Bold(true).Render(string(r)))
			} else {
				sb.WriteString(theme.S().Base.Bold(true).Render(string(r)))
			}
		} else {
			if selected {
				sb.WriteString(theme.S().SelectedBase.Render(string(r)))
			} else {
				sb.WriteString(theme.S().Base.Render(string(r)))
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
	runes := []rune(query)
	var matches []fuzzyMatch
	for idx, word := range words {
		positions := matchPositions(runes, word)
		if positions == nil {
			continue
		}
		matches = append(matches, fuzzyMatch{
			Index:          idx,
			Score:          scoreMatch(positions),
			MatchedIndexes: positions,
		})
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

// Model methods for agent picker

func (m *Model) agentPickerMaxWidth() int {
	width := m.w - m.input.CommandPickerXOffset()
	if width <= 0 {
		width = m.w
	}
	if width <= 0 {
		width = minAgentPickerWidth
	}
	return width
}

func (m *Model) openAgentPicker() tea.Cmd {
	return m.ensureAgentPicker("")
}

func (m *Model) ensureAgentPicker(query string) tea.Cmd {
	maxWidth := m.agentPickerMaxWidth()
	if m.agentPicker == nil {
		agents, err := llm.ListAgents(context.Background())
		if err != nil {
			return util.ReportError(fmt.Errorf("list agents: %w", err))
		}
		m.agentPicker = newAgentPicker(agents, coreagent.All(), maxWidth, m.currentActiveAgentName(), m.currentCoreAgentID())
		m.agentPickerIsFocus = false
		if query != "" {
			m.agentPicker.Filter(query)
		}
	} else {
		m.agentPicker.SetMaxWidth(maxWidth)
		m.agentPicker.Filter(query)
	}
	return nil
}

func (m *Model) ensureFocusAgentPicker(query string) tea.Cmd {
	maxWidth := m.agentPickerMaxWidth()
	if m.agentPicker == nil {
		agents, err := llm.ListAgents(context.Background())
		if err != nil {
			return util.ReportError(fmt.Errorf("list agents: %w", err))
		}
		focusedAgent := ""
		if m.sidebar != nil {
			focusedAgent = m.sidebar.FocusedAgentName()
		}
		m.agentPicker = newFocusAgentPicker(agents, maxWidth, focusedAgent)
		m.agentPickerIsFocus = true
		if query != "" {
			m.agentPicker.Filter(query)
		}
	} else {
		m.agentPicker.SetMaxWidth(maxWidth)
		m.agentPicker.Filter(query)
	}
	return nil
}

func (m *Model) refreshAgentPickerFromInput() tea.Cmd {
	query, ok := agentQueryFromInput(m.input.Value())
	if !ok {
		m.agentPicker = nil
		return nil
	}

	val := strings.TrimLeft(m.input.Value(), " \t")
	if strings.HasPrefix(strings.ToLower(val), "/focus") {
		return m.ensureFocusAgentPicker(query)
	}
	return m.ensureAgentPicker(query)
}

func agentQueryFromInput(val string) (string, bool) {
	base := strings.TrimLeft(val, " \t")

	// Try /agent prefix first
	prefix := "/agent"
	if len(base) > len(prefix) && strings.ToLower(base[:len(prefix)]) == prefix {
		after := base[len(prefix):]
		if after != "" && (after[0] == ' ' || after[0] == '\t') {
			rest := strings.TrimLeft(after, " \t")
			return rest, true
		}
	}

	// Try /focus prefix
	prefix = "/focus"
	if len(base) > len(prefix) && strings.ToLower(base[:len(prefix)]) == prefix {
		after := base[len(prefix):]
		if after != "" && (after[0] == ' ' || after[0] == '\t') {
			rest := strings.TrimLeft(after, " \t")
			return rest, true
		}
	}

	return "", false
}

func (m *Model) applyAgentPickerSelection() tea.Cmd {
	if m.agentPicker == nil {
		return nil
	}

	inputVal := m.input.Value()
	isFocusCommand := strings.HasPrefix(strings.TrimSpace(inputVal), "/focus")

	opt, ok := m.agentPicker.selected()
	m.agentPicker = nil
	if !ok {
		if query, has := agentQueryFromInput(inputVal); has && strings.TrimSpace(query) != "" {
			if isFocusCommand {
				return m.handleFocusCommand("/focus " + query)
			}
			return m.handleAgentCommand("/agent " + query)
		}
		return nil
	}

	var command string
	if isFocusCommand {
		if strings.TrimSpace(opt.name) == "" {
			go func() {
				ctx := context.Background()
				args := `{"agent_name": ""}`
				tooling.RunFocusAgent(ctx, args)
			}()
			return func() tea.Msg {
				m.input.SetValue("")
				return nil
			}
		} else {
			command = "/focus " + opt.name
		}
		return tea.Batch(m.handleFocusCommand(command), func() tea.Msg {
			m.input.SetValue("")
			return nil
		})
	} else {
		if strings.TrimSpace(opt.name) == "" {
			command = "/agent clear"
		} else {
			command = "/agent " + opt.name
		}
		return tea.Batch(m.handleAgentCommand(command), func() tea.Msg {
			m.input.SetValue("")
			return nil
		})
	}
}

func (m *Model) handleAgentPickerKey(keyStr string) (tea.Cmd, bool) {
	if m.agentPicker == nil {
		return nil, false
	}
	switch keyStr {
	case "esc", "ctrl+g":
		m.agentPicker = nil
		return nil, true
	case "up", "k", "ctrl+p", "ctrl+k":
		m.agentPicker.move(-1)
		return nil, true
	case "down", "j", "ctrl+n", "ctrl+j":
		m.agentPicker.move(1)
		return nil, true
	case "pgup":
		m.agentPicker.move(-5)
		return nil, true
	case "pgdown":
		m.agentPicker.move(5)
		return nil, true
	case "tab":
		m.agentPicker.move(1)
		return nil, true
	case "shift+tab":
		m.agentPicker.move(-1)
		return nil, true
	case "enter":
		return m.applyAgentPickerSelection(), true
	case "ctrl+c":
		m.agentPicker = nil
		return nil, false
	}
	return nil, false
}

func (m *Model) cycleActiveAgent() tea.Cmd {
	m.agentPicker = nil
	coreID := strings.TrimSpace(m.currentCoreAgentID())
	activeAgent := strings.TrimSpace(m.currentActiveAgentName())

	return func() tea.Msg {
		agents, err := llm.ListAgents(context.Background())
		if err != nil {
			return cycleAgentResultMsg{err: fmt.Errorf("list agents: %w", err)}
		}

		type agentOption struct {
			kind  string
			value string
			label string
		}

		coreDefs := coreagent.All()
		options := make([]agentOption, 0, len(agents)+len(coreDefs))
		seen := make(map[string]struct{})

		for _, def := range coreDefs {
			key := "core:" + strings.ToLower(def.ID)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			options = append(options, agentOption{kind: "core", value: def.ID, label: def.Name})
		}
		coreCount := len(options)

		remoteOpts := make([]agentOption, 0, len(agents))
		for _, agent := range agents {
			if !strings.EqualFold(strings.TrimSpace(agent.Status), "running") {
				continue
			}
			trimmed := strings.TrimSpace(agent.Name)
			if trimmed == "" {
				continue
			}
			key := "remote:" + strings.ToLower(trimmed)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			remoteOpts = append(remoteOpts, agentOption{kind: "remote", value: agent.Name, label: agent.Name})
		}

		sort.Slice(remoteOpts, func(i, j int) bool {
			return strings.ToLower(remoteOpts[i].label) < strings.ToLower(remoteOpts[j].label)
		})
		options = append(options, remoteOpts...)

		remoteCount := len(options) - coreCount
		clearActive := remoteCount == 0 && activeAgent != ""

		if len(options) == 0 {
			if clearActive {
				return cycleAgentResultMsg{clearActive: true}
			}
			return nil
		}

		currentKind := "remote"
		currentValue := activeAgent
		if currentValue == "" {
			currentKind = "core"
			currentValue = coreID
		}

		idx := 0
		for i, opt := range options {
			if opt.kind == currentKind && strings.EqualFold(opt.value, currentValue) {
				idx = i
				break
			}
		}

		next := options[(idx+1)%len(options)]
		if next.kind == "core" {
			return cycleAgentResultMsg{clearActive: clearActive, coreID: next.value}
		}

		// Return agent name without fetching metadata (will be fetched async)
		return cycleAgentResultMsg{clearActive: clearActive, meta: llm.AgentMetadata{Name: next.value}, hasMeta: true}
	}
}

func (m *Model) handleCycleAgentResult(msg cycleAgentResultMsg) tea.Cmd {
	if msg.clearActive && strings.TrimSpace(m.currentActiveAgentName()) != "" {
		m.clearActiveAgent()
		if m.sidebar != nil && m.currentCoreAgentID() == coreagent.IDBuilder {
			m.sidebar.SetFocusedAgent("")
		}
	}
	if msg.err != nil {
		return util.ReportError(msg.err)
	}
	var cmds []tea.Cmd
	if msg.coreID != "" {
		if m.agents != nil {
			if cmd := m.agents.switchCoreAgent(msg.coreID); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	} else if msg.hasMeta {
		// Set optimistic state and fetch metadata async
		if m.agents != nil {
			m.agents.setActiveAgentPending(msg.meta.Name)
			// Trigger refreshes via the controller's refresh functions
			if m.agents.refreshHeader != nil {
				m.agents.refreshHeader()
			}
			if m.agents.refreshSidebar != nil {
				m.agents.refreshSidebar()
			}
			cmds = append(cmds, m.agents.fetchAgentMetadataCmd(msg.meta.Name))
		}
	} else if msg.warn != "" {
		cmds = append(cmds, util.ReportWarn(msg.warn))
	}

	_ = m.refreshSidebar()
	m.refreshHeaderMeta()

	// If switching to Builder and there's a focused agent, fetch its metadata and set status
	if msg.coreID == coreagent.IDBuilder && m.sidebar != nil {
		if focusedAgent := m.sidebar.FocusedAgentName(); strings.TrimSpace(focusedAgent) != "" {
			// Set status from agentStatuses map if available
			if status, ok := m.agentStatuses[focusedAgent]; ok {
				m.sidebar.SetFocusedAgentStatus(status)
			}
			// Fetch metadata (commands, description)
			cmds = append(cmds, m.fetchFocusedAgentMetadataCmd(focusedAgent))
		}
	}

	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	default:
		return tea.Batch(cmds...)
	}
}
