package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"

	cmpmessages "tui/components/messages"
	tooltypes "tui/tools/types"
)

type toolDetailOverlay struct {
	id   string
	view *cmpmessages.ToolDetailView
}

func newToolDetailOverlay(call tooltypes.Call, result tooltypes.Result, width, height int) *toolDetailOverlay {
	overlay := &toolDetailOverlay{view: cmpmessages.NewToolDetailView()}
	overlay.view.SetFrameSize(width, height)
	overlay.view.SetData(call, result)
	overlay.id = strings.TrimSpace(call.ID)
	return overlay
}

func (o *toolDetailOverlay) SetSize(width, height int) {
	if o == nil || o.view == nil {
		return
	}
	o.view.SetFrameSize(width, height)
}

func (o *toolDetailOverlay) SetData(call tooltypes.Call, result tooltypes.Result) tea.Cmd {
	if o == nil {
		return nil
	}
	if o.view == nil {
		o.view = cmpmessages.NewToolDetailView()
	}
	o.id = strings.TrimSpace(call.ID)
	return o.view.SetData(call, result)
}

func (o *toolDetailOverlay) Update(msg tea.Msg) tea.Cmd {
	if o == nil || o.view == nil {
		return nil
	}
	return o.view.Update(msg)
}

func (o *toolDetailOverlay) View() string {
	if o == nil || o.view == nil {
		return ""
	}
	return o.view.View()
}

func (o *toolDetailOverlay) CallID() string {
	if o == nil {
		return ""
	}
	return o.id
}
