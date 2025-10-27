package streaming

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"

	tooltypes "tui/tools/types"
)

// State captures the in-flight stream for a session.
type State struct {
	Cancel    context.CancelFunc
	Channel   chan tea.Msg
	Canceling bool
	Waiting   bool
}

type PendingAssistant struct {
	Content string
	Waiting bool
}

// Manager centralizes streaming coordination for sessions.
type Manager struct {
	streams            map[string]*State
	pendingToolCalls   map[string]map[string]tooltypes.Call
	pendingToolOrder   map[string][]string
	pendingAssistants  map[string]*PendingAssistant
	pendingAsyncResumes map[string]bool // tracks sessions with completed async results waiting to resume
}

// NewManager constructs an initialized stream manager.
func NewManager() *Manager {
	return &Manager{
		streams:             make(map[string]*State),
		pendingToolCalls:    make(map[string]map[string]tooltypes.Call),
		pendingToolOrder:    make(map[string][]string),
		pendingAssistants:   make(map[string]*PendingAssistant),
		pendingAsyncResumes: make(map[string]bool),
	}
}

func (m *Manager) State(sessionID string) *State {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return m.streams[sessionID]
}

func (m *Manager) SetState(sessionID string, state *State) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if state == nil {
		delete(m.streams, sessionID)
		return
	}
	if m.streams == nil {
		m.streams = make(map[string]*State)
	}
	m.streams[sessionID] = state
}

func (m *Manager) Clear(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	delete(m.streams, sessionID)
	delete(m.pendingToolCalls, sessionID)
	delete(m.pendingToolOrder, sessionID)
	delete(m.pendingAssistants, sessionID)
	// NOTE: We intentionally do NOT clear pendingAsyncResumes here.
	// It's managed separately by completeResponse() to handle the async resume flow.
}

// IsBusy reports whether the session currently has an active stream.
func (m *Manager) IsBusy(sessionID string) bool {
	state := m.State(sessionID)
	return state != nil && state.Channel != nil
}

// IsCanceling reports whether the session is waiting for cancel confirmation.
func (m *Manager) IsCanceling(sessionID string) bool {
	state := m.State(sessionID)
	return state != nil && state.Canceling
}

func (m *Manager) States() map[string]*State {
	if m == nil || len(m.streams) == 0 {
		return nil
	}
	copy := make(map[string]*State, len(m.streams))
	for id, state := range m.streams {
		copy[id] = state
	}
	return copy
}

func (m *Manager) MarkCanceling(sessionID string, canceling bool) {
	state := m.State(sessionID)
	if state == nil {
		return
	}
	state.Canceling = canceling
}

// TrackToolCall records a pending tool call for a session.
func (m *Manager) TrackToolCall(sessionID string, call tooltypes.Call) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if m.pendingToolCalls == nil {
		m.pendingToolCalls = make(map[string]map[string]tooltypes.Call)
	}
	calls, ok := m.pendingToolCalls[sessionID]
	if !ok {
		calls = make(map[string]tooltypes.Call)
	}
	if _, exists := calls[call.ID]; !exists {
		m.pendingToolOrder[sessionID] = append(m.pendingToolOrder[sessionID], call.ID)
	}
	call.Finished = false
	calls[call.ID] = call
	m.pendingToolCalls[sessionID] = calls
}

// SetToolReason updates the reason for a tracked tool call.
func (m *Manager) SetToolReason(sessionID, callID, reason string) {
	sessionID = strings.TrimSpace(sessionID)
	callID = strings.TrimSpace(callID)
	reason = strings.TrimSpace(reason)
	if sessionID == "" || callID == "" || reason == "" {
		return
	}
	calls, ok := m.pendingToolCalls[sessionID]
	if !ok {
		return
	}
	call, exists := calls[callID]
	if !exists {
		return
	}
	if strings.TrimSpace(call.Reason) == reason {
		return
	}
	call.Reason = reason
	calls[callID] = call
}

func (m *Manager) ClearToolCall(sessionID, callID string) {
	sessionID = strings.TrimSpace(sessionID)
	callID = strings.TrimSpace(callID)
	if sessionID == "" || callID == "" {
		return
	}
	calls, ok := m.pendingToolCalls[sessionID]
	if !ok {
		return
	}
	delete(calls, callID)
	if len(calls) == 0 {
		delete(m.pendingToolCalls, sessionID)
		delete(m.pendingToolOrder, sessionID)
		return
	}
	order := m.pendingToolOrder[sessionID]
	for i, id := range order {
		if id == callID {
			order = append(order[:i], order[i+1:]...)
			break
		}
	}
	m.pendingToolOrder[sessionID] = order
}

func (m *Manager) ClearToolTracking(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	delete(m.pendingToolCalls, sessionID)
	delete(m.pendingToolOrder, sessionID)
}

func (m *Manager) ClearToolCallByID(callID string) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	for sessionID, calls := range m.pendingToolCalls {
		if _, ok := calls[callID]; ok {
			m.ClearToolCall(sessionID, callID)
			return
		}
	}
}

func (m *Manager) PendingToolCalls(sessionID string) (map[string]tooltypes.Call, []string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	calls := m.pendingToolCalls[sessionID]
	order := m.pendingToolOrder[sessionID]
	return calls, order
}

// BeginAssistant initializes a pending assistant stream.
func (m *Manager) BeginAssistant(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if m.pendingAssistants == nil {
		m.pendingAssistants = make(map[string]*PendingAssistant)
	}
	state := m.pendingAssistants[sessionID]
	if state == nil {
		state = &PendingAssistant{}
		m.pendingAssistants[sessionID] = state
	}
	state.Content = ""
	state.Waiting = true
}

// RecordAssistantContent appends streamed assistant text.
func (m *Manager) RecordAssistantContent(sessionID, delta string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || delta == "" {
		return
	}
	state := m.pendingAssistants[sessionID]
	if state == nil {
		state = &PendingAssistant{}
		m.pendingAssistants[sessionID] = state
	}
	state.Content += delta
	state.Waiting = false
}

func (m *Manager) MarkAssistantStreaming(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	state := m.pendingAssistants[sessionID]
	if state == nil {
		state = &PendingAssistant{}
		m.pendingAssistants[sessionID] = state
	}
	state.Waiting = false
}

func (m *Manager) PendingAssistant(sessionID string) *PendingAssistant {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	return m.pendingAssistants[sessionID]
}

// MarkPendingAsyncResume marks a session as having completed async results waiting to resume.
func (m *Manager) MarkPendingAsyncResume(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if m.pendingAsyncResumes == nil {
		m.pendingAsyncResumes = make(map[string]bool)
	}
	m.pendingAsyncResumes[sessionID] = true
}

// HasPendingAsyncResume checks if a session has completed async results waiting to resume.
func (m *Manager) HasPendingAsyncResume(sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	return m.pendingAsyncResumes[sessionID]
}

// ClearPendingAsyncResume clears the pending async resume flag for a session.
func (m *Manager) ClearPendingAsyncResume(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	delete(m.pendingAsyncResumes, sessionID)
}
