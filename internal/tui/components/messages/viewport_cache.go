package messages

import (
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	blockSeparator    = "\n\n"
	blockSpacingLines = 1
)

// blockCache stores the rendered output for a single message/tool block.
type blockCache struct {
	content     string
	height      int
	skip        bool
	placeholder string
	maxWidth    int
}

// viewportCache stores the aggregated layout information for all visible blocks.
type viewportCache struct {
	itemIdxs     []int
	heights      []int
	blockOffsets []int
	segments     []string
	visible      []bool
	totalHeight  int
	content      string
	blockSpacing int
	wrapEnabled  bool
	contentWidth int
}

// ViewportCache manages the rendering cache for message blocks with viewport-aware optimization.
type ViewportCache struct {
	renderCache  []blockCache
	vpCache      viewportCache
	dirtyAll     bool
	dirtyItems   map[int]struct{}
	layoutDirty  bool
}

// NewViewportCache creates a new viewport cache manager.
func NewViewportCache() *ViewportCache {
	return &ViewportCache{
		dirtyItems: make(map[int]struct{}),
		dirtyAll:   true,
		layoutDirty: true,
	}
}

// ensureMaps ensures the dirty tracking maps are initialized.
func (vc *ViewportCache) ensureMaps() {
	if vc.dirtyItems == nil {
		vc.dirtyItems = make(map[int]struct{})
	}
}

// MarkDirtyAll marks all items as needing re-render.
func (vc *ViewportCache) MarkDirtyAll() {
	vc.ensureMaps()
	vc.dirtyAll = true
	vc.layoutDirty = true
	for k := range vc.dirtyItems {
		delete(vc.dirtyItems, k)
	}
}

// MarkDirty marks a specific item as needing re-render.
func (vc *ViewportCache) MarkDirty(idx int, itemCount int) {
	if idx < 0 || idx >= itemCount {
		return
	}
	vc.ensureMaps()
	if vc.dirtyAll {
		return
	}
	vc.dirtyItems[idx] = struct{}{}
	vc.layoutDirty = true
}

// SyncCacheLen ensures the render cache matches the item count.
func (vc *ViewportCache) SyncCacheLen(itemCount int) {
	if len(vc.renderCache) < itemCount {
		delta := itemCount - len(vc.renderCache)
		vc.renderCache = append(vc.renderCache, make([]blockCache, delta)...)
		vc.layoutDirty = true
	} else if len(vc.renderCache) > itemCount {
		vc.renderCache = vc.renderCache[:itemCount]
		vc.layoutDirty = true
	}
}

// RenderBlock renders a single message component into a blockCache.
func (vc *ViewportCache) RenderBlock(idx int, cmp MessageCmp) blockCache {
	var bc blockCache
	if cmp == nil {
		return bc
	}

	// Check if this is an empty assistant message (should be skipped)
	if m, ok := cmp.(*messageCmp); ok && m.isAssistant() {
		if strings.TrimSpace(m.content()) == "" && !m.thinking {
			bc.skip = true
			return bc
		}
	}

	view := cmp.View()
	bc.content = view
	bc.height = lipgloss.Height(view)
	bc.placeholder = placeholderString(bc.height)

	lines := strings.Split(view, "\n")
	for _, line := range lines {
		width := ansi.StringWidth(line)
		if width > bc.maxWidth {
			bc.maxWidth = width
		}
	}
	return bc
}

// placeholderString creates a placeholder of the given height for lazy rendering.
func placeholderString(height int) string {
	if height <= 1 {
		return ""
	}
	return strings.Repeat("\n", height-1)
}

// ProcessDirtyItems renders all dirty items using the provided renderer function.
func (vc *ViewportCache) ProcessDirtyItems(items []MessageCmp) {
	vc.SyncCacheLen(len(items))

	if vc.dirtyAll {
		for i := range items {
			vc.renderCache[i] = vc.RenderBlock(i, items[i])
		}
		vc.dirtyAll = false
		for k := range vc.dirtyItems {
			delete(vc.dirtyItems, k)
		}
	} else if len(vc.dirtyItems) > 0 {
		for idx := range vc.dirtyItems {
			if idx < 0 || idx >= len(vc.renderCache) {
				continue
			}
			vc.renderCache[idx] = vc.RenderBlock(idx, items[idx])
		}
		for k := range vc.dirtyItems {
			delete(vc.dirtyItems, k)
		}
	}
}

// RenderableCount returns the number of non-skipped items.
func (vc *ViewportCache) RenderableCount() int {
	count := 0
	for _, bc := range vc.renderCache {
		if !bc.skip {
			count++
		}
	}
	return count
}

// NeedsWrap checks if any content exceeds the given width limit.
func (vc *ViewportCache) NeedsWrap(widthLimit int) bool {
	for _, bc := range vc.renderCache {
		if bc.skip {
			continue
		}
		if bc.maxWidth > widthLimit {
			return true
		}
	}
	return false
}

// RebuildViewportCache rebuilds the viewport cache structure with block offsets and segments.
func (vc *ViewportCache) RebuildViewportCache() {
	cache := viewportCache{
		blockSpacing: blockSpacingLines,
		wrapEnabled:  false,
	}

	for idx, bc := range vc.renderCache {
		if bc.skip {
			continue
		}
		cache.itemIdxs = append(cache.itemIdxs, idx)
		cache.heights = append(cache.heights, bc.height)
	}

	n := len(cache.itemIdxs)
	cache.blockOffsets = make([]int, n)
	cache.segments = make([]string, n)
	cache.visible = make([]bool, n)

	total := 0
	for i := 0; i < n; i++ {
		cache.blockOffsets[i] = total
		total += cache.heights[i]
		if i < n-1 {
			total += cache.blockSpacing
		}
		placeholder := vc.renderCache[cache.itemIdxs[i]].placeholder
		cache.segments[i] = placeholder
		cache.visible[i] = false
	}
	cache.totalHeight = total
	cache.content = joinSegments(cache.segments)
	vc.vpCache = cache
}

// UpdateViewportVisibility updates which blocks are visible based on viewport scroll position.
// Returns true if the visibility changed.
func (vc *ViewportCache) UpdateViewportVisibility(viewTop, vpHeight int, ensureVisibleIdx int) bool {
	cache := &vc.vpCache
	if cache.wrapEnabled || len(cache.itemIdxs) == 0 {
		return false
	}

	if vpHeight < 1 {
		vpHeight = 1
	}
	if viewTop < 0 {
		viewTop = 0
	}

	maxOffset := max(cache.totalHeight-vpHeight, 0)
	if viewTop > maxOffset {
		viewTop = maxOffset
	}

	viewBottom := viewTop + vpHeight
	if viewBottom > cache.totalHeight {
		viewBottom = cache.totalHeight
	}

	buffer := vpHeight / 2
	if buffer < 4 {
		buffer = 4
	}

	startLine := viewTop - buffer
	if startLine < 0 {
		startLine = 0
	}
	endLine := viewBottom + buffer
	if endLine > cache.totalHeight {
		endLine = cache.totalHeight
	}

	changed := false
	for i, idx := range cache.itemIdxs {
		blockTop := cache.blockOffsets[i]
		blockBottom := blockTop + cache.heights[i]
		shouldBeVisible := blockBottom >= startLine && blockTop <= endLine

		if ensureVisibleIdx >= 0 && idx == ensureVisibleIdx {
			shouldBeVisible = true
		}

		bc := vc.renderCache[idx]
		if shouldBeVisible != cache.visible[i] {
			cache.visible[i] = shouldBeVisible
			if shouldBeVisible {
				cache.segments[i] = bc.content
			} else {
				cache.segments[i] = bc.placeholder
			}
			changed = true
			continue
		}

		if shouldBeVisible {
			if cache.segments[i] != bc.content {
				cache.segments[i] = bc.content
				changed = true
			}
		} else if cache.segments[i] != bc.placeholder {
			cache.segments[i] = bc.placeholder
			changed = true
		}
	}

	if changed {
		cache.content = joinSegments(cache.segments)
	}
	return changed
}

// BuildWrappedContent builds viewport content with soft wrapping enabled.
func (vc *ViewportCache) BuildWrappedContent() string {
	segments := make([]string, 0, vc.RenderableCount())
	for _, bc := range vc.renderCache {
		if bc.skip {
			continue
		}
		segments = append(segments, bc.content)
	}
	return joinSegments(segments)
}

// joinSegments joins block segments with the standard block separator.
func joinSegments(segments []string) string {
	switch len(segments) {
	case 0:
		return ""
	case 1:
		return segments[0]
	}
	var builder strings.Builder
	for i, seg := range segments {
		if i > 0 {
			builder.WriteString(blockSeparator)
		}
		builder.WriteString(seg)
	}
	return builder.String()
}

// GetViewportCache returns the current viewport cache for external access.
func (vc *ViewportCache) GetViewportCache() viewportCache {
	return vc.vpCache
}

// SetViewportCache updates the viewport cache (for wrap mode transitions).
func (vc *ViewportCache) SetViewportCache(cache viewportCache) {
	vc.vpCache = cache
}

// IsLayoutDirty returns whether the layout needs rebuilding.
func (vc *ViewportCache) IsLayoutDirty() bool {
	return vc.layoutDirty
}

// ClearLayoutDirty marks the layout as clean.
func (vc *ViewportCache) ClearLayoutDirty() {
	vc.layoutDirty = false
}

// GetRenderCache returns the render cache for a specific index.
func (vc *ViewportCache) GetRenderCache(idx int) (blockCache, bool) {
	if idx < 0 || idx >= len(vc.renderCache) {
		return blockCache{}, false
	}
	return vc.renderCache[idx], true
}
