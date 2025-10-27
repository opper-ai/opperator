package timeutil

import (
	"fmt"
	"time"
)

// FormatRelativeTime formats a time duration in a human-readable way
// Similar to date-fns formatDistance function
func FormatRelativeTime(t time.Time, now time.Time) string {
	if t.IsZero() {
		return ""
	}

	duration := t.Sub(now)
	absDuration := duration
	if absDuration < 0 {
		absDuration = -absDuration
	}

	seconds := int(absDuration.Seconds())
	minutes := int(absDuration.Minutes())
	hours := int(absDuration.Hours())
	days := int(absDuration.Hours() / 24)

	switch {
	case seconds < 30:
		if duration < 0 {
			return "just now"
		}
		return "in a few seconds"
	case seconds < 90:
		if duration < 0 {
			return "a minute ago"
		}
		return "in a minute"
	case minutes < 45:
		if duration < 0 {
			return fmt.Sprintf("%d minutes ago", minutes)
		}
		return fmt.Sprintf("in %d minutes", minutes)
	case minutes < 90:
		if duration < 0 {
			return "an hour ago"
		}
		return "in an hour"
	case hours < 24:
		if duration < 0 {
			return fmt.Sprintf("%d hours ago", hours)
		}
		return fmt.Sprintf("in %d hours", hours)
	case days == 1:
		if duration < 0 {
			return "yesterday"
		}
		return "tomorrow"
	case days < 7:
		if duration < 0 {
			return fmt.Sprintf("%d days ago", days)
		}
		return fmt.Sprintf("in %d days", days)
	default:
		// For longer durations, show the actual date
		if duration < 0 {
			return fmt.Sprintf("on %s", t.Format("Jan 2"))
		}
		return fmt.Sprintf("on %s", t.Format("Jan 2"))
	}
}

// FormatNextRun formats the next run time for scheduled tasks
func FormatNextRun(nextRun time.Time, now time.Time) string {
	if nextRun.IsZero() {
		return ""
	}

	duration := nextRun.Sub(now)

	if duration <= 5*time.Second {
		return "soon"
	}

	return FormatRelativeTime(nextRun, now)
}

// FormatDuration formats a duration in a compact way
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	} else {
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd", days)
	}
}
