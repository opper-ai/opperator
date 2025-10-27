package message

import (
    "context"
    "fmt"
    "sync"
    "time"
)

type InMemoryService struct {
    mu   sync.Mutex
    seq  int64
    data map[string][]Message // sessionID -> messages
}

func NewInMemoryService() *InMemoryService { return &InMemoryService{data: map[string][]Message{}} }

func (s *InMemoryService) nextID() string {
    s.seq++
    return fmt.Sprintf("m-%d", s.seq)
}

func (s *InMemoryService) Create(ctx context.Context, sessionID string, params CreateMessageParams) (Message, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    now := time.Now().Unix()
    m := Message{
        ID:        s.nextID(),
        SessionID: sessionID,
        Role:      params.Role,
        Parts:     params.Parts,
        CreatedAt: now,
        UpdatedAt: now,
    }
    s.data[sessionID] = append(s.data[sessionID], m)
    return m, nil
}

func (s *InMemoryService) List(ctx context.Context, sessionID string) ([]Message, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    msgs := s.data[sessionID]
    out := make([]Message, len(msgs))
    copy(out, msgs)
    return out, nil
}
