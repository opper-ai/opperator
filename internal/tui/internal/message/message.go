package message

import (
	"context"
	"time"
)

// Role identifies who authored a message.
type Role string

const (
	Assistant Role = "assistant"
	User      Role = "user"
	System    Role = "system"
	Tool      Role = "tool"

	ToolCallRole         Role = "tool_call"
	ToolCallResponseRole Role = "tool_call_response"
)

type Content struct{ text string }

func (c Content) String() string { return c.text }

type Message struct {
	ID        string
	SessionID string
	Role      Role
	Parts     []ContentPart
	CreatedAt int64
	UpdatedAt int64
}

// Content() is defined in content.go to return TextContent.

func NewAssistant(text string) Message {
	return Message{Role: Assistant, Parts: []ContentPart{TextContent{Text: text}}, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
}

func NewUser(text string) Message {
	return Message{Role: User, Parts: []ContentPart{TextContent{Text: text}}, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
}

// CreateMessageParams are used for storing messages.
type CreateMessageParams struct {
	Role  Role
	Parts []ContentPart
}

// Service provides minimal multi-turn storage hooks.
type Service interface {
	Create(ctx context.Context, sessionID string, params CreateMessageParams) (Message, error)
	List(ctx context.Context, sessionID string) ([]Message, error)
	DeleteBySession(ctx context.Context, sessionID string) error
}
