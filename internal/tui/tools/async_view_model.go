package tools

// take this as input and return strings without side effects.
type AsyncViewModel struct {
	// Label is the display name for this async operation (e.g., "Agent", "Command")
	Label string

	// Status is the current state: "Running", "Completed", "Failed", "Pending"
	Status string

	// Lines are the progress messages to display
	Lines []string

	// ShowSpinner indicates whether to display a spinner animation
	ShowSpinner bool
}
