package tools

import "context"

type contextKey string

const (
	contextKeySessionID   contextKey = "tools.session_id"
	contextKeyCallID      contextKey = "tools.call_id"
	contextKeyActiveAgent contextKey = "tools.active_agent"
	contextKeyCoreAgent   contextKey = "tools.core_agent"
)

func WithSessionContext(ctx context.Context, sessionID, callID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if sessionID != "" {
		ctx = context.WithValue(ctx, contextKeySessionID, sessionID)
	}
	if callID != "" {
		ctx = context.WithValue(ctx, contextKeyCallID, callID)
	}
	return ctx
}

// SessionIDFromContext extracts the async session identifier if present.
func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if val, ok := ctx.Value(contextKeySessionID).(string); ok {
		return val
	}
	return ""
}

// CallIDFromContext extracts the async call identifier if present.
func CallIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if val, ok := ctx.Value(contextKeyCallID).(string); ok {
		return val
	}
	return ""
}

// WithAgentContext adds active and core agent information to the context.
func WithAgentContext(ctx context.Context, activeAgent, coreAgent string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if activeAgent != "" {
		ctx = context.WithValue(ctx, contextKeyActiveAgent, activeAgent)
	}
	if coreAgent != "" {
		ctx = context.WithValue(ctx, contextKeyCoreAgent, coreAgent)
	}
	return ctx
}

// ActiveAgentFromContext extracts the active agent name if present.
func ActiveAgentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if val, ok := ctx.Value(contextKeyActiveAgent).(string); ok {
		return val
	}
	return ""
}

// CoreAgentFromContext extracts the core agent ID if present.
func CoreAgentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if val, ok := ctx.Value(contextKeyCoreAgent).(string); ok {
		return val
	}
	return ""
}
