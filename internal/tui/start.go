package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"
)

// scrollEventFilter throttles mouse wheel events at the event loop level
// to prevent the queue from filling up with thousands of scroll events
var lastScrollEventTime time.Time

func scrollEventFilter(_ tea.Model, msg tea.Msg) tea.Msg {
	if _, ok := msg.(tea.MouseWheelMsg); ok {
		now := time.Now()
		if !lastScrollEventTime.IsZero() {
			// Drop mouse wheel events that arrive faster than 8ms apart (~120fps)
			// This prevents the event queue from filling up with thousands of events
			// while allowing responsive scrolling
			if now.Sub(lastScrollEventTime) < 8*time.Millisecond {
				return nil // Drop this event entirely - don't even queue it
			}
		}
		lastScrollEventTime = now
	}
	return msg
}

func Start() error {
	model, err := New()
	if err != nil {
		return err
	}
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseAllMotion(),              // Enable mouse motion events for text selection
		tea.WithFilter(scrollEventFilter), // Filter events before they enter the queue
	)
	_, err = p.Run()
	return err
}
