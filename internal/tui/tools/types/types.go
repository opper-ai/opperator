package types

// Call captures a tool invocation emitted by the model/agent stream.
type Call struct {
	ID       string
	Name     string
	Input    string
	Finished bool
	Reason   string
}

type Result struct {
	ToolCallID string
	Name       string
	Content    string
	Metadata   string
	IsError    bool
	Pending    bool
}
