package messages

import (
	"strings"
	"time"

	"tui/internal/message"
)

type turnMeta struct {
	agentID    string
	agentName  string
	agentColor string
	duration   time.Duration
}

func extractTurnMeta(parts []message.ContentPart) *turnMeta {
	if len(parts) == 0 {
		return nil
	}
	for _, part := range parts {
		if ts, ok := part.(message.TurnSummary); ok {
			if strings.TrimSpace(ts.AgentID) == "" && ts.DurationMilli <= 0 && strings.TrimSpace(ts.AgentName) == "" && strings.TrimSpace(ts.AgentColor) == "" {
				continue
			}
			return &turnMeta{
				agentID:    strings.TrimSpace(ts.AgentID),
				agentName:  strings.TrimSpace(ts.AgentName),
				agentColor: strings.TrimSpace(ts.AgentColor),
				duration:   time.Duration(ts.DurationMilli) * time.Millisecond,
			}
		}
	}
	return nil
}
