package permission

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"tui/internal/csync"
	"tui/internal/pubsub"
)

// ErrorPermissionDenied reports that the user rejected a permission request.
var ErrorPermissionDenied = errors.New("permission denied")

type CreatePermissionRequest struct {
	SessionID   string `json:"session_id"`
	ToolCallID  string `json:"tool_call_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
	Reason      string `json:"reason"`
}

// PermissionNotification provides status updates for an in-flight request.
type PermissionNotification struct {
	ToolCallID string `json:"tool_call_id"`
	Granted    bool   `json:"granted"`
	Denied     bool   `json:"denied"`
}

type PermissionRequest struct {
	ID          string `json:"id"`
	SessionID   string `json:"session_id"`
	ToolCallID  string `json:"tool_call_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
	Reason      string `json:"reason"`
}

// Service coordinates permission requests between the engine and the UI.
type Service interface {
	pubsub.Subscriber[PermissionRequest]
	GrantPersistent(permission PermissionRequest)
	Grant(permission PermissionRequest)
	Deny(permission PermissionRequest)
	Request(opts CreatePermissionRequest) bool
	AutoApproveSession(sessionID string)
	SetSkipRequests(skip bool)
	SkipRequests() bool
	SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[PermissionNotification]
}

// NewService constructs a service rooted in workingDir. Paths relative to
// workingDir are expanded before comparison.
func NewService(workingDir string, skip bool, allowedTools []string) Service {
	return &permissionService{
		Broker:              pubsub.NewBroker[PermissionRequest](),
		notificationBroker:  pubsub.NewBroker[PermissionNotification](),
		workingDir:          workingDir,
		sessionPermissions:  make([]PermissionRequest, 0),
		autoApproveSessions: make(map[string]bool),
		skip:                skip,
		allowedTools:        allowedTools,
		pendingRequests:     csync.NewMap[string, chan bool](),
	}
}

type permissionService struct {
	*pubsub.Broker[PermissionRequest]

	notificationBroker    *pubsub.Broker[PermissionNotification]
	workingDir            string
	sessionPermissions    []PermissionRequest
	sessionPermissionsMu  sync.RWMutex
	pendingRequests       *csync.Map[string, chan bool]
	autoApproveSessions   map[string]bool
	autoApproveSessionsMu sync.RWMutex
	skip                  bool
	allowedTools          []string

	requestMu     sync.Mutex
	activeRequest *PermissionRequest
}

func (s *permissionService) GrantPersistent(permission PermissionRequest) {
	s.publishGrant(permission)

	s.sessionPermissionsMu.Lock()
	alreadyAllowed := false
	for _, existing := range s.sessionPermissions {
		if existing.SessionID == permission.SessionID && existing.ToolName == permission.ToolName {
			alreadyAllowed = true
			break
		}
	}
	if !alreadyAllowed {
		s.sessionPermissions = append(s.sessionPermissions, PermissionRequest{
			SessionID: permission.SessionID,
			ToolName:  permission.ToolName,
		})
	}
	s.sessionPermissionsMu.Unlock()

	if s.activeRequest != nil && s.activeRequest.ID == permission.ID {
		s.activeRequest = nil
	}
}

func (s *permissionService) Grant(permission PermissionRequest) {
	s.publishGrant(permission)

	if s.activeRequest != nil && s.activeRequest.ID == permission.ID {
		s.activeRequest = nil
	}
}

func (s *permissionService) publishGrant(permission PermissionRequest) {
	s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
		ToolCallID: permission.ToolCallID,
		Granted:    true,
	})
	if respCh, ok := s.pendingRequests.Get(permission.ID); ok {
		respCh <- true
	}
}

func (s *permissionService) Deny(permission PermissionRequest) {
	s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
		ToolCallID: permission.ToolCallID,
		Granted:    false,
		Denied:     true,
	})
	if respCh, ok := s.pendingRequests.Get(permission.ID); ok {
		respCh <- false
	}

	if s.activeRequest != nil && s.activeRequest.ID == permission.ID {
		s.activeRequest = nil
	}
}

func (s *permissionService) Request(opts CreatePermissionRequest) bool {
	if s.skip {
		return true
	}

	s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
		ToolCallID: opts.ToolCallID,
	})

	s.requestMu.Lock()
	defer s.requestMu.Unlock()

	commandKey := opts.ToolName + ":" + opts.Action
	if slices.Contains(s.allowedTools, commandKey) || slices.Contains(s.allowedTools, opts.ToolName) {
		return true
	}

	s.autoApproveSessionsMu.RLock()
	autoApprove := s.autoApproveSessions[opts.SessionID]
	s.autoApproveSessionsMu.RUnlock()
	if autoApprove {
		return true
	}

	dir := s.normalizePath(opts.Path)
	permission := PermissionRequest{
		ID:          uuid.New().String(),
		Path:        dir,
		SessionID:   opts.SessionID,
		ToolCallID:  opts.ToolCallID,
		ToolName:    opts.ToolName,
		Description: opts.Description,
		Action:      opts.Action,
		Params:      opts.Params,
		Reason:      opts.Reason,
	}

	if s.isPreviouslyGranted(permission) {
		return true
	}

	s.activeRequest = &permission

	respCh := make(chan bool, 1)
	s.pendingRequests.Set(permission.ID, respCh)
	defer s.pendingRequests.Del(permission.ID)

	s.Publish(pubsub.CreatedEvent, permission)

	return <-respCh
}

func (s *permissionService) normalizePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return s.workingDir
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	if s.workingDir == "" {
		return filepath.Clean(trimmed)
	}
	return filepath.Clean(filepath.Join(s.workingDir, trimmed))
}

func (s *permissionService) isPreviouslyGranted(permission PermissionRequest) bool {
	s.sessionPermissionsMu.RLock()
	defer s.sessionPermissionsMu.RUnlock()
	for _, p := range s.sessionPermissions {
		if p.SessionID == permission.SessionID && p.ToolName == permission.ToolName {
			return true
		}
	}
	return false
}

func (s *permissionService) AutoApproveSession(sessionID string) {
	s.autoApproveSessionsMu.Lock()
	s.autoApproveSessions[sessionID] = true
	s.autoApproveSessionsMu.Unlock()
}

func (s *permissionService) SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[PermissionNotification] {
	return s.notificationBroker.Subscribe(ctx)
}

func (s *permissionService) SetSkipRequests(skip bool) {
	s.skip = skip
}

func (s *permissionService) SkipRequests() bool {
	return s.skip
}
