package messages

import (
	"tui/styles"

	"github.com/charmbracelet/bubbles/v2/viewport"
	"github.com/charmbracelet/lipgloss/v2"
)

// LayoutRenderer handles layout calculation, scroll management, and final viewport rendering.
type LayoutRenderer struct {
	cache *ViewportCache
}

// NewLayoutRenderer creates a new layout renderer with the given cache.
func NewLayoutRenderer(cache *ViewportCache) *LayoutRenderer {
	return &LayoutRenderer{
		cache: cache,
	}
}

// Render builds the final viewport content with layout and scroll management.
// Returns the rendered content and whether scrollToBottom should be triggered.
func (lr *LayoutRenderer) Render(
	vp *viewport.Model,
	width int,
	wasAtBottom bool,
	ensureVisibleIdx int,
) (content string, scrollToBottom bool) {
	renderableCount := lr.cache.RenderableCount()
	widthLimit := vp.Width()
	if widthLimit <= 0 {
		widthLimit = width
	}

	// Check if we need soft wrapping
	needsWrap := lr.cache.NeedsWrap(widthLimit)
	layoutChanged := false
	if vp.SoftWrap != needsWrap {
		vp.SoftWrap = needsWrap
		layoutChanged = true
	}

	vpCache := lr.cache.GetViewportCache()

	// Handle empty content
	if renderableCount == 0 {
		placeholder := emptyPlaceholderBlock(width)
		if vpCache.content != placeholder || lr.cache.IsLayoutDirty() || !vpCache.wrapEnabled || vpCache.contentWidth != widthLimit {
			newCache := viewportCache{
				content:      placeholder,
				wrapEnabled:  true,
				contentWidth: widthLimit,
			}
			lr.cache.SetViewportCache(newCache)
			vp.SetContent(placeholder)
		}
		lr.cache.ClearLayoutDirty()
		return vp.View(), false
	}

	// Handle soft-wrap mode (simpler rendering)
	if needsWrap {
		contentChanged := lr.cache.IsLayoutDirty() || layoutChanged || !vpCache.wrapEnabled || vpCache.contentWidth != widthLimit
		if contentChanged {
			content := lr.cache.BuildWrappedContent()
			newCache := viewportCache{
				content:      content,
				wrapEnabled:  true,
				contentWidth: widthLimit,
			}
			lr.cache.SetViewportCache(newCache)
			vp.SetContent(content)
		}

		scrollToBottom := false
		if ensureVisibleIdx >= 0 {
			scrollToBottom = true
		} else if wasAtBottom && contentChanged {
			scrollToBottom = true
		}

		if scrollToBottom {
			vp.GotoBottom()
		}
		lr.cache.ClearLayoutDirty()
		return vp.View(), false
	}

	// Handle non-wrap mode with visibility culling
	if vpCache.wrapEnabled {
		layoutChanged = true
	}

	if lr.cache.IsLayoutDirty() || len(vpCache.itemIdxs) != renderableCount || vpCache.wrapEnabled || vpCache.contentWidth != widthLimit {
		lr.cache.RebuildViewportCache()
		vpCache = lr.cache.GetViewportCache()
		vpCache.contentWidth = widthLimit
		lr.cache.SetViewportCache(vpCache)
		layoutChanged = true
	}

	vpHeightCurrent := max(vp.Height(), 1)
	viewTop := vp.YOffset()
	visibilityChanged := lr.cache.UpdateViewportVisibility(viewTop, vpHeightCurrent, ensureVisibleIdx)

	if layoutChanged || visibilityChanged {
		vpCache = lr.cache.GetViewportCache()
		vp.SetContent(vpCache.content)
	}

	// Handle ensureVisibleIdx scrolling
	handledEnsure := false
	if ensureVisibleIdx >= 0 && len(vpCache.itemIdxs) > 0 {
		blockIdx := -1
		for j, idx := range vpCache.itemIdxs {
			if idx == ensureVisibleIdx {
				blockIdx = j
				break
			}
		}

		if blockIdx == -1 {
			for j := len(vpCache.itemIdxs) - 1; j >= 0; j-- {
				if vpCache.itemIdxs[j] < ensureVisibleIdx {
					blockIdx = j
					break
				}
			}
			if blockIdx == -1 {
				blockIdx = 0
			}
		}

		if blockIdx >= 0 && blockIdx < len(vpCache.heights) {
			top := vpCache.blockOffsets[blockIdx]
			h := vpCache.heights[blockIdx]
			if h < 1 {
				h = 1
			}
			visibleH := max(vp.Height(), 1)
			cur := vp.YOffset()
			if top < cur {
				vp.SetYOffset(top)
			} else if top+h > cur+visibleH {
				vp.SetYOffset(top + h - visibleH)
			}
			handledEnsure = true
		}
	}

	if !handledEnsure && wasAtBottom && (layoutChanged || visibilityChanged) {
		scrollToBottom = true
	}

	lr.cache.ClearLayoutDirty()
	return vp.View(), scrollToBottom
}

// emptyPlaceholderBlock renders the placeholder shown when there are no messages.
func emptyPlaceholderBlock(width int) string {
	t := styles.CurrentTheme()
	lines := []string{
		t.S().Muted.Italic(true).Render("No messages yet."),
		t.S().Muted.Render(""),
		t.S().Subtle.Render("Start by sending a message to get going."),
	}
	block := lipgloss.JoinVertical(lipgloss.Left, lines...)
	if width <= 0 {
		return block
	}
	return lipgloss.NewStyle().PaddingLeft(2).Width(width).Render(block)
}

// FindBlockPosition returns the viewport offset and height for a given item index.
// Returns -1, -1 if not found.
func (lr *LayoutRenderer) FindBlockPosition(itemIdx int) (offset int, height int) {
	vpCache := lr.cache.GetViewportCache()
	for i, idx := range vpCache.itemIdxs {
		if idx == itemIdx {
			return vpCache.blockOffsets[i], vpCache.heights[i]
		}
	}
	return -1, -1
}
