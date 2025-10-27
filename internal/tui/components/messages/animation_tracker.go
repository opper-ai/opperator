package messages

import (
	"sort"

	"tui/components/anim"

	tea "github.com/charmbracelet/bubbletea/v2"
)

// AnimationTracker manages which message items need animation updates.
type AnimationTracker struct {
	animatedItems    map[int]struct{}
	initializedItems map[int]struct{}
}

// NewAnimationTracker creates a new animation tracker.
func NewAnimationTracker() *AnimationTracker {
	return &AnimationTracker{
		animatedItems:    make(map[int]struct{}),
		initializedItems: make(map[int]struct{}),
	}
}

// ensureMaps ensures the tracking maps are initialized.
func (at *AnimationTracker) ensureMaps() {
	if at.animatedItems == nil {
		at.animatedItems = make(map[int]struct{})
	}
	if at.initializedItems == nil {
		at.initializedItems = make(map[int]struct{})
	}
}

// Track updates animation tracking for a specific item.
// Determines if the item should be animated based on its state.
func (at *AnimationTracker) Track(idx int, cmp MessageCmp) {
	if idx < 0 || cmp == nil {
		return
	}
	at.ensureMaps()

	shouldAnimate := false
	switch c := cmp.(type) {
	case *messageCmp:
		if c.isAssistant() && c.thinking {
			shouldAnimate = true
		}
	case ToolCallCmp:
		if c.Animating() {
			shouldAnimate = true
		}
	}

	if shouldAnimate {
		at.animatedItems[idx] = struct{}{}
	} else {
		delete(at.animatedItems, idx)
		delete(at.initializedItems, idx)
	}
}

// Untrack removes an item from animation tracking.
func (at *AnimationTracker) Untrack(idx int) {
	at.ensureMaps()
	delete(at.animatedItems, idx)
	delete(at.initializedItems, idx)
}

// GetTargetsForUpdate returns the indices of items that should receive an update message.
// This includes the focused item and all animating items.
func (at *AnimationTracker) GetTargetsForUpdate(msg tea.Msg, focusIdx int, itemCount int) []int {
	if itemCount == 0 {
		return nil
	}

	var indices []int
	switch msg.(type) {
	case tea.KeyPressMsg:
		if focusIdx >= 0 && focusIdx < itemCount {
			indices = append(indices, focusIdx)
		}
	case anim.StepMsg:
		// handled below; animated items updated regardless
	default:
		if focusIdx >= 0 && focusIdx < itemCount {
			indices = append(indices, focusIdx)
		}
	}

	for idx := range at.animatedItems {
		indices = append(indices, idx)
	}

	return at.normalizeIndices(indices, itemCount)
}

// normalizeIndices sorts and deduplicates indices, ensuring they're within bounds.
func (at *AnimationTracker) normalizeIndices(indices []int, itemCount int) []int {
	if len(indices) == 0 {
		return nil
	}
	sort.Ints(indices)
	out := indices[:0]
	last := -1
	for _, idx := range indices {
		if idx < 0 || idx >= itemCount {
			continue
		}
		if len(out) == 0 || idx != last {
			out = append(out, idx)
			last = idx
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// InitializeAnimatingItems sends Init() commands to any animating items that haven't been initialized yet.
// Returns a batch command if any items need initialization.
func (at *AnimationTracker) InitializeAnimatingItems(items []MessageCmp) tea.Cmd {
	var cmds []tea.Cmd
	for idx := range at.animatedItems {
		if idx < 0 || idx >= len(items) {
			continue
		}
		if _, alreadyInitialized := at.initializedItems[idx]; alreadyInitialized {
			continue
		}
		if toolCmp, ok := items[idx].(ToolCallCmp); ok {
			if toolCmp.Animating() {
				if initCmd := toolCmp.Init(); initCmd != nil {
					cmds = append(cmds, initCmd)
					at.initializedItems[idx] = struct{}{}
				}
			}
		}
	}
	if len(cmds) > 0 {
		return tea.Batch(cmds...)
	}
	return nil
}

// IsAnimating returns whether the given index is currently being animated.
func (at *AnimationTracker) IsAnimating(idx int) bool {
	_, ok := at.animatedItems[idx]
	return ok
}

// AnimatingCount returns the number of items currently being animated.
func (at *AnimationTracker) AnimatingCount() int {
	return len(at.animatedItems)
}

// Clear removes all animation tracking.
func (at *AnimationTracker) Clear() {
	at.ensureMaps()
	for k := range at.animatedItems {
		delete(at.animatedItems, k)
	}
	for k := range at.initializedItems {
		delete(at.initializedItems, k)
	}
}

// TrackAppendedItem tracks animation for a newly appended item.
// Returns an Init() command if the item is animating.
func (at *AnimationTracker) TrackAppendedItem(idx int, cmp MessageCmp) tea.Cmd {
	at.Track(idx, cmp)

	// For tool components, return their Init() if they're animating
	if toolCmp, ok := cmp.(ToolCallCmp); ok {
		if toolCmp.Animating() {
			return toolCmp.Init()
		}
	}

	// For message components with thinking state
	if msgCmp, ok := cmp.(*messageCmp); ok {
		if msgCmp.isAssistant() && msgCmp.thinking {
			return msgCmp.Init()
		}
	}

	return nil
}

// UpdateAfterEntryChange re-tracks an item after its entry data has changed.
// Returns an Init() command if animation state transitioned from off to on.
func (at *AnimationTracker) UpdateAfterEntryChange(idx int, cmp MessageCmp, wasAnimating bool) tea.Cmd {
	if idx < 0 || cmp == nil {
		return nil
	}

	toolCmp, ok := cmp.(ToolCallCmp)
	if !ok {
		at.Track(idx, cmp)
		return nil
	}

	nowAnimating := toolCmp.Animating()
	at.Track(idx, cmp)

	if nowAnimating && !wasAnimating {
		return toolCmp.Init()
	}
	return nil
}

// GetAnimatedIndices returns a sorted slice of all animated item indices.
func (at *AnimationTracker) GetAnimatedIndices() []int {
	indices := make([]int, 0, len(at.animatedItems))
	for idx := range at.animatedItems {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	return indices
}
