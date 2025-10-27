package secretprompt

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"tui/internal/csync"
	"tui/internal/pubsub"
)

// ErrCanceled is returned when a prompt request is canceled before collecting a value.
var ErrCanceled = errors.New("secret prompt canceled")

type CreateRequest struct {
	SessionID        string
	ToolCallID       string
	Name             string
	Mode             string
	Title            string
	Description      string
	ValueLabel       string
	DocumentationURL string
	DefaultValue     string
	Error            string
}

// PromptRequest captures the state exposed to subscribers when a new prompt is created.
type PromptRequest struct {
	ID               string    `json:"id"`
	SessionID        string    `json:"session_id"`
	ToolCallID       string    `json:"tool_call_id"`
	Name             string    `json:"name"`
	Mode             string    `json:"mode"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	ValueLabel       string    `json:"value_label"`
	DocumentationURL string    `json:"documentation_url"`
	DefaultValue     string    `json:"default_value"`
	Error            string    `json:"error"`
	CreatedAt        time.Time `json:"created_at"`
}

// Service coordinates prompt requests between background workers and the UI overlay.
type Service interface {
	pubsub.Subscriber[PromptRequest]

	Request(ctx context.Context, opts CreateRequest) (string, error)
	Resolve(id string, value string)
	Reject(id string, err error)
	CancelSession(sessionID string)
}

type promptResult struct {
	value string
	err   error
}

type promptService struct {
	*pubsub.Broker[PromptRequest]

	pending *csync.Map[string, chan promptResult]
	reqs    *csync.Map[string, PromptRequest]
}

// NewService constructs a secret prompt service.
func NewService() Service {
	return &promptService{
		Broker:  pubsub.NewBroker[PromptRequest](),
		pending: csync.NewMap[string, chan promptResult](),
		reqs:    csync.NewMap[string, PromptRequest](),
	}
}

func (s *promptService) Request(ctx context.Context, opts CreateRequest) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	id := uuid.New().String()
	req := PromptRequest{
		ID:               id,
		SessionID:        strings.TrimSpace(opts.SessionID),
		ToolCallID:       strings.TrimSpace(opts.ToolCallID),
		Name:             strings.TrimSpace(opts.Name),
		Mode:             strings.TrimSpace(opts.Mode),
		Title:            strings.TrimSpace(opts.Title),
		Description:      strings.TrimSpace(opts.Description),
		ValueLabel:       strings.TrimSpace(opts.ValueLabel),
		DocumentationURL: strings.TrimSpace(opts.DocumentationURL),
		DefaultValue:     opts.DefaultValue,
		Error:            strings.TrimSpace(opts.Error),
		CreatedAt:        time.Now(),
	}

	resultCh := make(chan promptResult, 1)
	s.pending.Set(id, resultCh)
	s.reqs.Set(id, req)

	s.Publish(pubsub.CreatedEvent, req)

	select {
	case <-ctx.Done():
		s.cleanup(id)
		s.Publish(pubsub.DeletedEvent, req)
		return "", ctx.Err()
	case res := <-resultCh:
		s.cleanup(id)
		if res.err != nil {
			return "", res.err
		}
		return res.value, nil
	}
}

func (s *promptService) Resolve(id string, value string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	if ch, ok := s.pending.Take(id); ok {
		ch <- promptResult{value: value}
	}
	if req, ok := s.reqs.Take(id); ok {
		s.Publish(pubsub.DeletedEvent, req)
	}
}

func (s *promptService) Reject(id string, err error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	if err == nil {
		err = ErrCanceled
	}
	if ch, ok := s.pending.Take(id); ok {
		ch <- promptResult{err: err}
	}
	if req, ok := s.reqs.Take(id); ok {
		s.Publish(pubsub.DeletedEvent, req)
	}
}

func (s *promptService) CancelSession(sessionID string) {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return
	}
	s.reqs.Range(func(id string, req PromptRequest) bool {
		if strings.EqualFold(req.SessionID, trimmed) {
			if _, ok := s.pending.Get(id); ok {
				s.Reject(id, ErrCanceled)
			}
		}
		return true
	})
}

func (s *promptService) cleanup(id string) {
	s.pending.Del(id)
	s.reqs.Del(id)
}
