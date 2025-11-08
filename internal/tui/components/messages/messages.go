package messages

import (
	"fmt"
	"os"
	"strings"
	"time"

	"tui/components/anim"
	"tui/internal/message"
	tooltypes "tui/tools/types"
	"tui/toolstate"
	"tui/util"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/viewport"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
)

// Debug logging function
func logToFile(format string, args ...interface{}) {
	f, err := os.OpenFile("/tmp/selection-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, format, args...)
}

type Messages struct {
	w, h                  int
	vp                    viewport.Model
	inited                bool
	items                 []MessageCmp
	focus                 int // -1 when none
	ensureVisibleIdx      int // -1 when none
	conversationStartTime time.Time
	assistantName         string
	assistantColor        string
	assistantID           string
	toolStore             *toolstate.Store
	toolIndex             map[string]int
	waitingForAssistant   bool
	activeHiddenLoads     map[string]struct{}

	cache       *ViewportCache
	renderer    *LayoutRenderer
	animator    *AnimationTracker
	lastUserIdx int
	screenTop   int
}

const (
	permissionDeniedResultContent = "Request has been cancelled.\nTell me what to do differently."
)

func (c *Messages) markDirtyAll() {
	c.cache.MarkDirtyAll()
}

func (c *Messages) markDirty(idx int) {
	c.cache.MarkDirty(idx, len(c.items))
}

func (c *Messages) spinnerRequired(msg *messageCmp) bool {
	if c.animator != nil && c.animator.HasVisibleToolAnimating(c.items) {
		return false
	}
	hasContent := false
	if msg != nil && strings.TrimSpace(msg.content()) != "" {
		hasContent = true
	}
	if c.waitingForAssistant {
		return true
	}
	if !hasContent && len(c.activeHiddenLoads) > 0 {
		return true
	}
	return false
}

func (c *Messages) latestAssistant() (int, *messageCmp) {
	for i := len(c.items) - 1; i >= 0; i-- {
		if msg, ok := c.items[i].(*messageCmp); ok && msg.isAssistant() {
			return i, msg
		}
	}
	return -1, nil
}

func (c *Messages) syncAssistantSpinner() tea.Cmd {
	idx, msg := c.latestAssistant()
	if msg == nil {
		return nil
	}
	shouldThink := c.spinnerRequired(msg)
	if msg.thinking == shouldThink {
		if shouldThink {
			c.animator.Track(idx, msg)
			return c.animator.InitializeItem(idx, msg)
		}
		return nil
	}
	msg.setThinking(shouldThink)
	if shouldThink {
		if c.conversationStartTime.IsZero() {
			c.conversationStartTime = time.Now()
		}
		if msg.startedAt.IsZero() {
			msg.startedAt = c.conversationStartTime
		}
	}
	c.markDirty(idx)
	c.animator.Track(idx, msg)
	if shouldThink {
		return c.animator.InitializeItem(idx, msg)
	}
	return nil
}

func (c *Messages) setHiddenToolLoading(id string, active bool) tea.Cmd {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if c.activeHiddenLoads == nil {
		c.activeHiddenLoads = make(map[string]struct{})
	}
	_, exists := c.activeHiddenLoads[id]
	switch {
	case active && !exists:
		c.activeHiddenLoads[id] = struct{}{}
	case !active && exists:
		delete(c.activeHiddenLoads, id)
	default:
		return nil
	}
	return c.syncAssistantSpinner()
}

func (c *Messages) updateHiddenToolLoading(entry toolstate.Execution) tea.Cmd {
	return c.setHiddenToolLoading(entry.Call.ID, !entry.Finished())
}

func (c *Messages) trackHiddenTool(entry toolstate.Execution) {
	id := strings.TrimSpace(entry.Call.ID)
	if id == "" {
		return
	}
	if c.toolIndex == nil {
		c.toolIndex = make(map[string]int)
	}
	c.toolIndex[id] = -1
	if c.activeHiddenLoads == nil {
		c.activeHiddenLoads = make(map[string]struct{})
	}
}

func (c *Messages) appendItem(cmp MessageCmp) int {
	if cmp == nil {
		return -1
	}
	cmp.SetSize(c.w, 0)
	c.items = append(c.items, cmp)
	idx := len(c.items) - 1
	if idx < 0 {
		return idx
	}
	c.markDirty(idx)
	c.animator.Track(idx, cmp)
	if toolCmp, ok := cmp.(ToolCallCmp); ok {
		entry := toolCmp.Entry()
		if id := strings.TrimSpace(entry.Call.ID); id != "" {
			if c.toolIndex == nil {
				c.toolIndex = make(map[string]int)
			}
			c.toolIndex[id] = idx
		}
	}
	if msgCmp, ok := cmp.(*messageCmp); ok {
		if msgCmp.role() == message.User {
			c.lastUserIdx = idx
		}
	}
	return idx
}

func (c *Messages) isAssistantText(idx int) bool {
	if idx < 0 || idx >= len(c.items) {
		return false
	}
	if msg, ok := c.items[idx].(*messageCmp); ok && msg.isAssistant() {
		return strings.TrimSpace(msg.content()) != "" || msg.thinking
	}
	return false
}

func (c *Messages) isFocusable(idx int) bool {
	if c.isAssistantText(idx) {
		return true
	}
	if idx < 0 || idx >= len(c.items) {
		return false
	}
	if _, ok := c.items[idx].(ToolCallCmp); ok {
		return true
	}
	return false
}

func (c *Messages) findFocusableForward(start int) (int, bool) {
	for i := start + 1; i < len(c.items); i++ {
		if c.isFocusable(i) {
			return i, true
		}
	}
	return -1, false
}

func (c *Messages) findFocusableBackward(start int) (int, bool) {
	limit := start
	if limit > len(c.items) {
		limit = len(c.items)
	}
	for i := limit - 1; i >= 0; i-- {
		if c.isFocusable(i) {
			return i, true
		}
	}
	return -1, false
}

func (c *Messages) initIfNeeded() {
	if c.inited {
		return
	}
	c.vp = viewport.New()
	c.vp.MouseWheelEnabled = true
	c.vp.MouseWheelDelta = 5 // Scroll 5 lines per event for faster scrolling
	c.vp.SoftWrap = false
	c.vp.FillHeight = true
	c.inited = true
	c.focus = -1
	c.ensureVisibleIdx = -1
	c.lastUserIdx = -1
	if c.cache == nil {
		c.cache = NewViewportCache()
	}
	if c.renderer == nil {
		c.renderer = NewLayoutRenderer(c.cache)
	}
	if c.animator == nil {
		c.animator = NewAnimationTracker()
	}
	if c.toolStore == nil {
		c.toolStore = toolstate.NewStore()
	}
	if c.toolIndex == nil {
		c.toolIndex = make(map[string]int)
	}
}

// SetScreenTop records the screen-space Y coordinate where the messages viewport begins.
func (c *Messages) SetScreenTop(y int) {
	if y < 0 {
		y = 0
	}
	c.screenTop = y
}

func (c *Messages) SetSize(w, h int) {
	c.w, c.h = w, h
	c.initIfNeeded()
	c.vp.SetWidth(w)
	c.vp.SetHeight(h)
	// propagate width to all items
	for i := range c.items {
		c.items[i].SetSize(w, 0)
	}
	c.markDirtyAll()
}

// LoadConversation replaces current items with messages from storage.
func (c *Messages) LoadConversation(msgs []message.Message) {
	loadConversation(c, msgs)
}

func (c *Messages) addToolEntry(entry toolstate.Execution) tea.Cmd {
	if def, hasDef := resolveToolDefinition(entry); hasDef && def.Hidden {
		c.trackHiddenTool(entry)
		return c.updateHiddenToolLoading(entry)
	}
	cmp := NewToolCallCmp(entry)
	idx := c.appendItem(cmp)
	if idx >= 0 {
		c.ensureVisibleIdx = idx
	}
	var cmds []tea.Cmd
	if init := c.animator.TrackAppendedItem(idx, cmp); init != nil {
		cmds = append(cmds, init)
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (c *Messages) addOrReplaceToolEntry(entry toolstate.Execution) tea.Cmd {
	if def, hasDef := resolveToolDefinition(entry); hasDef && def.Hidden {
		c.trackHiddenTool(entry)
		return c.updateHiddenToolLoading(entry)
	}
	cmp := NewToolCallCmp(entry)
	if len(c.items) > 0 {
		if m, ok := c.items[len(c.items)-1].(*messageCmp); ok && m.isAssistant() {
			content := strings.TrimSpace(m.content())
			if content == "" {
				cmp.SetSize(c.w, 0)
				idx := len(c.items) - 1
				c.items[idx] = cmp
				c.markDirty(idx)
				c.animator.Track(idx, cmp)
				if c.toolIndex == nil {
					c.toolIndex = make(map[string]int)
				}
				c.toolIndex[strings.TrimSpace(entry.Call.ID)] = idx
				c.ensureVisibleIdx = idx
				var cmds []tea.Cmd
				if init := c.animator.TrackAppendedItem(idx, cmp); init != nil {
					cmds = append(cmds, init)
				}
				if len(cmds) == 0 {
					return nil
				}
				return tea.Batch(cmds...)
			}
		}
	}
	return c.addToolEntry(entry)
}

func (c *Messages) applyExistingToolEntry(entry toolstate.Execution, markDirty bool) tea.Cmd {
	id := strings.TrimSpace(entry.Call.ID)
	if id == "" {
		return nil
	}
	idx, ok := c.toolIndex[id]

	// Check if this is a hidden tool (idx == -1)
	if ok && idx == -1 {
		return c.updateHiddenToolLoading(entry)
	}

	if !ok || idx < 0 || idx >= len(c.items) {
		return c.addToolEntry(entry)
	}
	cmp, ok := c.items[idx].(ToolCallCmp)
	if !ok {
		return c.addToolEntry(entry)
	}
	wasAnimating := cmp.Animating()
	cmp.SetEntry(entry)
	if markDirty || wasAnimating != cmp.Animating() {
		c.markDirty(idx)
	}
	return c.animator.UpdateAfterEntryChange(idx, cmp, wasAnimating)
}

// AddUser appends a new user message.
func (c *Messages) AddUser(s string) {
	// Reset conversation timer for new user input
	c.conversationStartTime = time.Time{}
	cmp := newMessageCmp(message.User, s, c.w, false)
	idx := c.appendItem(cmp)
	if idx >= 0 {
		c.ensureVisibleIdx = idx
	}
}

// AddAssistantStart begins a new assistant message (typically followed by streaming deltas).
func (c *Messages) AddAssistantStart(model string) {
	cmp := newMessageCmp(message.Assistant, "", c.w, false)
	cmp.setAgentInfo(c.assistantID, c.assistantName, c.assistantColor)
	cmp.setThinking(true)
	idx := c.appendItem(cmp)
	if idx >= 0 {
		c.ensureVisibleIdx = idx
	}
}
func (c *Messages) StartLoading() tea.Cmd {
	if c.waitingForAssistant {
		return nil
	}
	c.waitingForAssistant = true
	return c.syncAssistantSpinner()
}

// StopLoading clears the thinking state on the latest assistant message.
func (c *Messages) StopLoading() tea.Cmd {
	if !c.waitingForAssistant {
		return nil
	}
	c.waitingForAssistant = false
	return c.syncAssistantSpinner()
}

// AppendAssistant appends text to the last assistant message; if absent, starts a new one.
func (c *Messages) AppendAssistant(s string) {
	if len(c.items) == 0 {
		c.AddAssistantStart("")
	}
	if m, ok := c.items[len(c.items)-1].(*messageCmp); ok && m.isAssistant() {
		m.appendContent(s)
		c.markDirty(len(c.items) - 1)
	} else {
		c.AddAssistantStart("")
		if m2, ok2 := c.items[len(c.items)-1].(*messageCmp); ok2 {
			m2.appendContent(s)
			c.markDirty(len(c.items) - 1)
		}
	}
}

// EndAssistant marks the current assistant message as finished (stop thinking block).
func (c *Messages) EndAssistant() {
	if len(c.items) == 0 {
		return
	}
	if m, ok := c.items[len(c.items)-1].(*messageCmp); ok && m.isAssistant() {
		m.setThinking(false)
		m.markFinished()
		idx := len(c.items) - 1
		c.markDirty(idx)
		c.animator.Track(idx, m)
	}
}

// SetActiveAssistantContent replaces the content on the current assistant
// message, if present. Used when restoring an in-flight response after the UI
// was reloaded (e.g. session switch) so we don't duplicate streamed text.
func (c *Messages) SetActiveAssistantContent(content string) {
	if len(c.items) == 0 {
		return
	}
	if m, ok := c.items[len(c.items)-1].(*messageCmp); ok && m.isAssistant() {
		m.setContent(content)
		c.markDirty(len(c.items) - 1)
	}
}

func (c *Messages) setFocus(idx int) bool {
	if idx < 0 || idx >= len(c.items) {
		c.ClearFocus()
		return false
	}
	if c.focus == idx {
		c.ensureVisibleIdx = idx
		return false
	}
	if c.focus >= 0 && c.focus < len(c.items) {
		c.items[c.focus].Blur()
		c.markDirty(c.focus)
	}
	c.focus = idx
	c.items[idx].Focus()
	c.markDirty(idx)
	c.ensureVisibleIdx = idx
	return true
}

func (c *Messages) ClearFocus() {
	if c.focus >= 0 && c.focus < len(c.items) {
		c.items[c.focus].Blur()
		c.markDirty(c.focus)
	}
	c.focus = -1
}
func (c *Messages) HasFocus() bool { return c.focus >= 0 }

func (c *Messages) FocusedToolCall() (tooltypes.Call, tooltypes.Result, bool) {
	entry, ok := c.FocusedToolEntry()
	if !ok {
		return tooltypes.Call{}, tooltypes.Result{}, false
	}
	return entry.Call, entry.Result, true
}

func (c *Messages) FocusedToolCallID() (string, bool) {
	call, _, ok := c.FocusedToolCall()
	if !ok {
		return "", false
	}
	id := strings.TrimSpace(call.ID)
	if id == "" {
		return "", false
	}
	return id, true
}

func (c *Messages) ToolCallByID(id string) (tooltypes.Call, tooltypes.Result, bool) {
	entry, ok := c.ToolEntryByID(id)
	if !ok {
		return tooltypes.Call{}, tooltypes.Result{}, false
	}
	return entry.Call, entry.Result, true
}

func (c *Messages) FocusedToolEntry() (toolstate.Execution, bool) {
	if c.focus < 0 || c.focus >= len(c.items) {
		return toolstate.Execution{}, false
	}
	if tc, ok := c.items[c.focus].(ToolCallCmp); ok {
		entry := tc.Entry()
		if strings.TrimSpace(entry.Call.ID) == "" {
			return toolstate.Execution{}, false
		}
		return entry, true
	}
	return toolstate.Execution{}, false
}

func (c *Messages) ToolEntryByID(id string) (toolstate.Execution, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return toolstate.Execution{}, false
	}
	c.initIfNeeded()
	if c.toolStore == nil {
		return toolstate.Execution{}, false
	}
	return c.toolStore.Entry(id)
}

func (c *Messages) FocusLast() bool {
	n := len(c.items)
	if n == 0 {
		c.focus = -1
		return false
	}
	for i := n - 1; i >= 0; i-- {
		if c.isFocusable(i) {
			return c.setFocus(i)
		}
	}
	c.ClearFocus()
	return false
}

func (c *Messages) FocusNext() bool {
	n := len(c.items)
	if n == 0 {
		c.focus = -1
		return false
	}
	prev := c.focus
	start := prev
	if start < 0 {
		start = -1
	}
	idx, ok := c.findFocusableForward(start)
	if !ok || idx == prev {
		return false
	}
	return c.setFocus(idx)
}

func (c *Messages) FocusPrev() bool {
	n := len(c.items)
	if n == 0 {
		c.focus = -1
		return false
	}
	prev := c.focus
	start := prev
	if start < 0 {
		start = n
	}
	idx, ok := c.findFocusableBackward(start)
	if !ok || idx == prev {
		return false
	}
	return c.setFocus(idx)
}

// stopThinking is false the spinner remains visible (e.g. while tools run).
func (c *Messages) StreamStarted(stopThinking bool) tea.Cmd {
	_, msg := c.latestAssistant()
	if msg == nil {
		return nil
	}
	if stopThinking {
		needsSync := false
		if c.waitingForAssistant {
			c.waitingForAssistant = false
			needsSync = true
		}
		if msg.startedAt.IsZero() {
			msg.markStarted()
		}
		if needsSync {
			return c.syncAssistantSpinner()
		}
		return nil
	}
	if strings.TrimSpace(msg.content()) == "" {
		if c.waitingForAssistant {
			return nil
		}
		c.waitingForAssistant = true
		return c.syncAssistantSpinner()
	}
	return nil
}

func (c *Messages) Update(msg tea.Msg) tea.Cmd {
	c.initIfNeeded()

	// Pass through to viewport for scrolling (event loop level throttling handles flood prevention)
	var cmd tea.Cmd
	c.vp, cmd = c.vp.Update(msg)
	var cmds []tea.Cmd
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Handle mouse events for text selection (after viewport handles scrolling)
	if mouseCmd := c.handleMouseEvent(msg); mouseCmd != nil {
		cmds = append(cmds, mouseCmd)
	}

	indices := c.animator.GetTargetsForUpdate(msg, c.focus, len(c.items))
	isStep := false
	if _, ok := msg.(anim.StepMsg); ok {
		isStep = true
	}
	for _, i := range indices {
		_, icmd := c.items[i].Update(msg)
		if icmd != nil {
			cmds = append(cmds, icmd)
		}
		if isStep {
			c.markDirty(i)
		}
	}

	// Auto-initialize any animating items that need it (only once per item)
	if initCmd := c.animator.InitializeAnimatingItems(c.items); initCmd != nil {
		cmds = append(cmds, initCmd)
	}

	if len(cmds) > 0 {
		return tea.Batch(cmds...)
	}
	return nil
}

func (c *Messages) HandleSelectionKeyPress(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	var selectedText string
	hasSelection := false

	for _, item := range c.items {
		msgCmp, ok := item.(*messageCmp)
		if !ok {
			continue
		}
		if msgCmp.selection.HasSelection() {
			if !hasSelection {
				selectedText = msgCmp.SelectedText()
				hasSelection = true
			}
		}
	}

	if !hasSelection {
		return nil, false
	}

	copyRequested := key.Matches(msg, CopyKey)
	c.clearAllSelections()

	if copyRequested {
		return selectionCopyCmd(selectedText), true
	}

	return nil, false
}

// handleMouseEvent processes mouse events for text selection.
// Returns a command if the event was handled and should stop propagating.
func (c *Messages) handleMouseEvent(msg tea.Msg) tea.Cmd {
	// Only handle mouse click, motion (drag), and release for selection
	var isMouseEvent bool
	var eventType string

	switch msg.(type) {
	case tea.MouseClickMsg:
		isMouseEvent = true
		eventType = "click"
	case tea.MouseMotionMsg:
		isMouseEvent = true
		eventType = "motion"
	case tea.MouseReleaseMsg:
		isMouseEvent = true
		eventType = "release"
	default:
		return nil
	}

	if !isMouseEvent {
		return nil
	}

	// Debug: Log mouse events
	var mouseY int
	if eventType == "click" {
		m := msg.(tea.MouseClickMsg).Mouse()
		mouseY = m.Y
		logToFile("\n=== MOUSE CLICK at x=%d y=%d ===\n", m.X, m.Y)
		logToFile("Viewport: yOffset=%d, height=%d\n", c.vp.YOffset(), c.vp.Height())
		logToFile("Total items: %d, focused: %d\n", len(c.items), c.focus)
	} else if eventType == "motion" {
		m := msg.(tea.MouseMotionMsg).Mouse()
		mouseY = m.Y
	} else if eventType == "release" {
		m := msg.(tea.MouseReleaseMsg).Mouse()
		mouseY = m.Y
	}

	// Debug: Log ALL message boundaries
	vpCache := c.cache.GetViewportCache()
	if eventType == "click" {
		logToFile("\nMessage boundaries:\n")
		for i, idx := range vpCache.itemIdxs {
			if idx < 0 || idx >= len(c.items) {
				continue
			}
			messageTopLocal := vpCache.blockOffsets[i] - c.vp.YOffset()
			screenTop := c.screenTop + messageTopLocal
			messageBottom := screenTop + vpCache.heights[i]

			// Check message type
			msgType := "unknown"
			if msgCmp, ok := c.items[idx].(*messageCmp); ok {
				if msgCmp.msg.Role == message.User {
					msgType = "User"
				} else {
					msgType = "Assistant"
				}
			}

			logToFile("  Message %d (%s): blockOffset=%d, height=%d, screen Y=%d to %d\n",
				idx, msgType, vpCache.blockOffsets[i], vpCache.heights[i],
				screenTop, messageBottom-1)
		}
		logToFile("\n")
	}

	// Find which message the mouse is over by checking Y coordinates
	// Loop through all visible messages in the viewport cache
	var targetIdx = -1
	var targetMessageY int

	for i, idx := range vpCache.itemIdxs {
		if idx < 0 || idx >= len(c.items) {
			continue
		}

		// Calculate message's screen position
		messageTopLocal := vpCache.blockOffsets[i] - c.vp.YOffset()
		messageTop := c.screenTop + messageTopLocal
		messageBottom := messageTop + vpCache.heights[i]

		// Check if mouse is within this message's bounds
		if mouseY >= messageTop && mouseY < messageBottom {
			targetIdx = idx
			targetMessageY = messageTop
			logToFile("✓ Selected message %d: mouseY=%d is in range [%d, %d)\n",
				idx, mouseY, messageTop, messageBottom)
			break
		}
	}

	if targetIdx < 0 {
		if eventType == "click" {
			c.clearAllSelections()
			logToFile("Cleared all selections (click outside messages)\n")
		}
		logToFile("Mouse not over any message\n")
		return nil
	}

	// Clear selections on other messages so we maintain a single active selection
	if eventType == "click" {
		c.clearSelectionsExcept(targetIdx)
		logToFile("Cleared selections on other messages\n")
	}

	// Only forward to messageCmp items (not tool calls)
	item := c.items[targetIdx]
	msgCmp, ok := item.(*messageCmp)
	if !ok {
		logToFile("Target item is not a messageCmp: type=%T\n", item)
		return nil
	}

	// Forward the mouse event to the message component
	cmd := msgCmp.HandleMouseEvent(msg, targetMessageY)

	// Mark the target message as dirty to trigger re-render with selection highlight
	c.markDirty(targetIdx)

	return cmd
}

// clearAllSelections clears text selection in all message components.
func (c *Messages) clearAllSelections() {
	for idx, item := range c.items {
		if msgCmp, ok := item.(*messageCmp); ok {
			if msgCmp.selection.HasSelection() {
				msgCmp.ClearSelection()
				c.markDirty(idx) // Re-render to remove highlight
			}
		}
	}
}

func (c *Messages) clearSelectionsExcept(target int) {
	for idx, item := range c.items {
		if idx == target {
			continue
		}
		if msgCmp, ok := item.(*messageCmp); ok {
			if msgCmp.selection.HasSelection() {
				msgCmp.ClearSelection()
				c.markDirty(idx)
			}
		}
	}
}

// HasSelection reports whether any message currently has a selection.
func (c *Messages) HasSelection() bool {
	for _, item := range c.items {
		if msgCmp, ok := item.(*messageCmp); ok && msgCmp.selection.HasSelection() {
			return true
		}
	}
	return false
}

func selectionCopyCmd(text string) tea.Cmd {
	return tea.Sequence(
		tea.SetClipboard(text),
		func() tea.Msg {
			_ = clipboard.WriteAll(text)
			return nil
		},
		util.ReportInfo("Text copied to clipboard"),
	)
}

func (c *Messages) View() string {
	c.initIfNeeded()

	// Header is now rendered at the top level, not here
	vpHeight := c.h
	if vpHeight < 1 {
		vpHeight = 1
	}
	c.vp.SetHeight(vpHeight)

	// Process dirty items through cache
	c.cache.ProcessDirtyItems(c.items)

	// Render using the layout renderer
	wasAtBottom := c.vp.AtBottom()
	view, scrollToBottom := c.renderer.Render(&c.vp, c.w, wasAtBottom, c.ensureVisibleIdx)

	if scrollToBottom {
		c.vp.GotoBottom()
	}

	// Clear ensureVisibleIdx after it's been processed
	c.ensureVisibleIdx = -1

	body := lipgloss.NewStyle().Width(c.w).Height(vpHeight).Render(view)
	return body
}

// Header control APIs

func (c *Messages) SetAssistantDefaults(id, name, color string) {
	c.initIfNeeded()
	trimmedID := strings.TrimSpace(id)
	trimmedName := strings.TrimSpace(name)
	trimmedColor := strings.TrimSpace(color)
	if c.assistantID == trimmedID && c.assistantName == trimmedName && c.assistantColor == trimmedColor {
		return
	}
	c.assistantID = trimmedID
	c.assistantName = trimmedName
	c.assistantColor = trimmedColor
	for i := range c.items {
		if msg, ok := c.items[i].(*messageCmp); ok && msg.isAssistant() {
			changed := false
			if msg.agentMatches(trimmedID) {
				changed = msg.setAgentInfo(trimmedID, trimmedName, trimmedColor)
			} else if msg.ensureAgentDefaults(trimmedID, trimmedName, trimmedColor) {
				changed = true
			}
			if changed {
				c.markDirty(i)
			}
		}
	}
}

// AddToolCall appends a new assistant entry with an embedded tool call component.
func (c *Messages) AddToolCall(tc tooltypes.Call) tea.Cmd {
	c.initIfNeeded()
	entry, changed, created := c.toolStore.EnsureCall(tc)
	if created {
		return c.addToolEntry(entry)
	}
	return c.applyExistingToolEntry(entry, changed)
}

// AddOrReplaceToolCall replaces the last assistant message with a tool call
// This is used for the first tool call to avoid empty assistant messages.
func (c *Messages) AddOrReplaceToolCall(tc tooltypes.Call) tea.Cmd {
	c.initIfNeeded()
	entry, changed, created := c.toolStore.EnsureCall(tc)
	if created {
		return c.addOrReplaceToolEntry(entry)
	}
	return c.applyExistingToolEntry(entry, changed)
}

// EnsureToolCall guarantees that a tool call with the given ID exists in the
// conversation, updating it in place when already present. Returns an init
// command if a new tool component was added.
func (c *Messages) EnsureToolCall(tc tooltypes.Call) tea.Cmd {
	entry, changed, created := c.toolStore.EnsureCall(tc)
	if created {
		return c.addOrReplaceToolEntry(entry)
	}
	return c.applyExistingToolEntry(entry, changed)
}

// marks its tool call as finished.
func (c *Messages) FinishTool(id string, result tooltypes.Result) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	entry, changed := c.toolStore.Complete(id, result)
	if strings.TrimSpace(entry.Call.ID) == "" {
		return
	}
	c.applyExistingToolEntry(entry, changed)
}

func (c *Messages) MarkToolPermissionRequested(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	entry, changed := c.toolStore.RequestPermission(id)
	if strings.TrimSpace(entry.Call.ID) == "" {
		return
	}
	c.applyExistingToolEntry(entry, changed)
}

func (c *Messages) MarkToolPermissionGranted(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	entry, changed := c.toolStore.GrantPermission(id)
	if strings.TrimSpace(entry.Call.ID) == "" {
		return
	}
	c.applyExistingToolEntry(entry, changed)
}

func (c *Messages) MarkToolPermissionDenied(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	entry, changed := c.toolStore.DenyPermission(id, permissionDeniedResultContent)
	if strings.TrimSpace(entry.Call.ID) == "" {
		return
	}
	c.applyExistingToolEntry(entry, changed)
}

func (c *Messages) SetToolReason(id string, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	entry, changed := c.toolStore.SetReason(id, reason)
	if strings.TrimSpace(entry.Call.ID) == "" {
		return
	}
	c.applyExistingToolEntry(entry, changed)
}

func (c *Messages) SetToolLifecycle(id string, lifecycle toolstate.Lifecycle) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	entry, changed := c.toolStore.SetLifecycle(id, lifecycle)
	if strings.TrimSpace(entry.Call.ID) == "" {
		return
	}
	c.applyExistingToolEntry(entry, changed)
}

func (c *Messages) SetToolFlags(id string, flags toolstate.ExecutionFlags) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	entry, changed := c.toolStore.SetFlags(id, flags)
	if strings.TrimSpace(entry.Call.ID) == "" {
		return
	}
	c.applyExistingToolEntry(entry, changed)
}

func (c *Messages) SetToolDisplay(id string, display toolstate.ExecutionDisplay) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	entry, changed := c.toolStore.SetDisplay(id, display)
	if strings.TrimSpace(entry.Call.ID) == "" {
		return
	}
	c.applyExistingToolEntry(entry, changed)
}

func (c *Messages) SetToolProgress(id string, entries []toolstate.ProgressRecord) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	entry, changed := c.toolStore.SetProgress(id, entries)
	if strings.TrimSpace(entry.Call.ID) == "" {
		return
	}
	c.applyExistingToolEntry(entry, changed)
}

func (c *Messages) AppendToolProgress(id string, entries []toolstate.ProgressRecord) {
	id = strings.TrimSpace(id)
	if id == "" || len(entries) == 0 {
		return
	}
	entry, changed := c.toolStore.AppendProgress(id, entries)
	if strings.TrimSpace(entry.Call.ID) == "" {
		return
	}
	c.applyExistingToolEntry(entry, changed)
}

// UpdateToolDelta appends input delta and/or updates the name for a pending tool call.
func (c *Messages) UpdateToolDelta(id, name, delta string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	var entry toolstate.Execution
	var changed bool
	if strings.TrimSpace(name) != "" {
		var created bool
		entry, changed, created = c.toolStore.EnsureCall(tooltypes.Call{ID: id, Name: name})
		if created {
			c.addOrReplaceToolEntry(entry)
		} else if changed {
			c.applyExistingToolEntry(entry, true)
		}
	}
	if delta != "" {
		entry, changed = c.toolStore.AppendInput(id, delta)
		if strings.TrimSpace(entry.Call.ID) == "" {
			return
		}
		c.applyExistingToolEntry(entry, changed)
	}
}

// SetPendingToolResult initializes or updates the tool result content while the call
// remains pending. No-op when the tool call has already finished.
func (c *Messages) SetPendingToolResult(id string, result tooltypes.Result) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	entry, changed := c.toolStore.SetPendingResult(id, result)
	if strings.TrimSpace(entry.Call.ID) == "" {
		return
	}
	c.applyExistingToolEntry(entry, changed)
}

// UpdateToolResultMeta parses the tool result metadata as a JSON object, calls
// is created. Safe to call while the tool is pending.
func (c *Messages) UpdateToolResultMeta(id string, update func(map[string]any) map[string]any) {
	entry, changed := c.toolStore.UpdateMetadata(id, update)
	if strings.TrimSpace(entry.Call.ID) == "" {
		return
	}
	c.applyExistingToolEntry(entry, changed)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
