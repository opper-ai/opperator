package toolstate

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"time"

	tooling "tui/tools"
	toolregistry "tui/tools/registry"
	tooltypes "tui/tools/types"
)

type PermissionState int

const (
	PermissionUnknown PermissionState = iota
	PermissionRequested
	PermissionGranted
	PermissionDenied
)

// Lifecycle captures the execution lifecycle of a tool call.
type Lifecycle int

const (
	LifecycleUnknown Lifecycle = iota
	LifecyclePending
	LifecycleRunning
	LifecycleCompleted
	LifecycleFailed
	LifecycleCancelled
	LifecycleDeleted
)

// Finished reports whether the lifecycle represents a terminal state.
func (l Lifecycle) Finished() bool {
	switch l {
	case LifecycleCompleted, LifecycleFailed, LifecycleCancelled, LifecycleDeleted:
		return true
	default:
		return false
	}
}

func (l Lifecycle) String() string {
	switch l {
	case LifecyclePending:
		return "pending"
	case LifecycleRunning:
		return "running"
	case LifecycleCompleted:
		return "completed"
	case LifecycleFailed:
		return "failed"
	case LifecycleCancelled:
		return "cancelled"
	case LifecycleDeleted:
		return "deleted"
	default:
		return "unknown"
	}
}

// ProgressRecord captures a single progress update associated with a tool call.
type ProgressRecord struct {
	Timestamp time.Time
	Text      string
	Status    string
	Metadata  string
}

// ExecutionFlags provide additional context about how the tool call should be treated.
type ExecutionFlags struct {
	Async       bool
	Persistent  bool
	NeedsResume bool
}

type ExecutionDisplay struct {
	Label   string
	Summary string
	Body    []string
}

type Execution struct {
	ID         string
	Tool       string
	Call       tooltypes.Call
	Result     tooltypes.Result
	Permission PermissionState

	Lifecycle   Lifecycle
	Flags       ExecutionFlags
	Progress    []ProgressRecord
	Display     ExecutionDisplay
	StartedAt   time.Time
	CompletedAt *time.Time
}

// Finished reports whether the tool call has finished.
func (e Execution) Finished() bool {
	if e.Lifecycle.Finished() {
		return true
	}
	return e.Call.Finished
}

// their state in a consistent manner. All operations are safe for concurrent use.
type Store struct {
	mu      sync.RWMutex
	entries map[string]Execution
}

func NewStore() *Store {
	return &Store{entries: make(map[string]Execution)}
}

// EnsureCall inserts or updates the state for a tool call, returning the
// normalized execution, a flag indicating whether the execution changed, and
// whether it was newly inserted.
func (s *Store) EnsureCall(call tooltypes.Call) (Execution, bool, bool) {
	id := strings.TrimSpace(call.ID)
	if id == "" {
		return Execution{}, false, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	exec, exists := s.entries[id]
	prev := exec

	if !exists {
		exec.ID = id
		exec.Call.ID = id
	}

	exec.Call = mergeCall(exec.Call, call)
	normalizeExecution(&exec)

	changed := !reflect.DeepEqual(exec, prev)
	if changed || !exists {
		s.entries[id] = exec
	}
	return exec, changed, !exists
}

// call has already finished the operation is a no-op.
func (s *Store) SetPendingResult(id string, result tooltypes.Result) (Execution, bool) {
	return s.mutate(id, func(exec *Execution) {
		if exec.Call.Finished || exec.Lifecycle.Finished() {
			return
		}
		exec.Result = mergePendingResult(*exec, result)
	})
}

// Complete merges the final result, marks the call finished, and returns the
// updated execution along with a flag indicating whether the execution changed.
func (s *Store) Complete(id string, result tooltypes.Result) (Execution, bool) {
	return s.mutate(id, func(exec *Execution) {
		exec.Result = mergeFinalResult(*exec, result)
		exec.Call.Finished = true
		if exec.Result.IsError {
			exec.Lifecycle = LifecycleFailed
		} else {
			exec.Lifecycle = LifecycleCompleted
		}
	})
}

// AppendInput appends delta to the call input and returns the updated execution.
func (s *Store) AppendInput(id, delta string) (Execution, bool) {
	if id = strings.TrimSpace(id); id == "" || delta == "" {
		return Execution{}, false
	}
	return s.mutate(id, func(exec *Execution) {
		exec.Call.Input += delta
	})
}

// SetReason updates the tool call reason when provided.
func (s *Store) SetReason(id, reason string) (Execution, bool) {
	reason = strings.TrimSpace(reason)
	if id = strings.TrimSpace(id); id == "" || reason == "" {
		return Execution{}, false
	}
	return s.mutate(id, func(exec *Execution) {
		exec.Call.Reason = reason
	})
}

// UpdateMetadata parses existing metadata, applies update, and stores the
// normalized JSON back onto the execution.
func (s *Store) UpdateMetadata(id string, update func(map[string]any) map[string]any) (Execution, bool) {
	if id = strings.TrimSpace(id); id == "" || update == nil {
		return Execution{}, false
	}
	return s.mutate(id, func(exec *Execution) {
		meta := ParseMetadata(exec.Result.Metadata)
		meta = update(cloneMap(meta))
		exec.Result.Metadata = encodeMetadata(meta)
	})
}

// RequestPermission marks the call as awaiting approval.
func (s *Store) RequestPermission(id string) (Execution, bool) {
	return s.setPermission(id, PermissionRequested, nil)
}

// GrantPermission marks the call permission as granted.
func (s *Store) GrantPermission(id string) (Execution, bool) {
	return s.setPermission(id, PermissionGranted, nil)
}

// DenyPermission marks the call permission as denied, optionally setting an
// error result message when none is present.
func (s *Store) DenyPermission(id, fallbackContent string) (Execution, bool) {
	return s.setPermission(id, PermissionDenied, func(exec *Execution) {
		exec.Call.Finished = true
		exec.Lifecycle = LifecycleFailed
		if strings.TrimSpace(exec.Result.ToolCallID) == "" {
			exec.Result.ToolCallID = exec.Call.ID
		}
		if strings.TrimSpace(exec.Result.Name) == "" {
			exec.Result.Name = exec.Call.Name
		}
		if strings.TrimSpace(exec.Result.Content) == "" {
			exec.Result.Content = fallbackContent
		}
		exec.Result.IsError = true
	})
}

func (s *Store) Entry(id string) (Execution, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Execution{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	exec, ok := s.entries[id]
	return exec, ok
}

// SetLifecycle explicitly sets the lifecycle for the execution, overriding the
// derived value until the next mutation implies a different state.
func (s *Store) SetLifecycle(id string, lifecycle Lifecycle) (Execution, bool) {
	return s.mutate(id, func(exec *Execution) {
		exec.Lifecycle = lifecycle
		exec.Call.Finished = lifecycle.Finished()
	})
}

// SetFlags merges the provided flags onto the execution flags.
func (s *Store) SetFlags(id string, flags ExecutionFlags) (Execution, bool) {
	return s.mutate(id, func(exec *Execution) {
		exec.Flags = mergeFlags(exec.Flags, flags)
	})
}

// SetDisplay merges the provided display overrides onto the execution.
func (s *Store) SetDisplay(id string, display ExecutionDisplay) (Execution, bool) {
	return s.mutate(id, func(exec *Execution) {
		exec.Display = mergeDisplay(exec.Display, display)
	})
}

// SetProgress replaces the progress entries for the execution.
func (s *Store) SetProgress(id string, entries []ProgressRecord) (Execution, bool) {
	return s.mutate(id, func(exec *Execution) {
		exec.Progress = copyProgressRecords(entries)
	})
}

// AppendProgress appends progress entries to the execution.
func (s *Store) AppendProgress(id string, entries []ProgressRecord) (Execution, bool) {
	if len(entries) == 0 {
		return s.Entry(id)
	}
	return s.mutate(id, func(exec *Execution) {
		exec.Progress = append(exec.Progress, copyProgressRecords(entries)...)
		if exec.Lifecycle == LifecycleUnknown || exec.Lifecycle == LifecyclePending {
			exec.Lifecycle = LifecycleRunning
		}
		if !exec.Lifecycle.Finished() {
			exec.Call.Finished = false
		}
	})
}

func (s *Store) setPermission(id string, state PermissionState, mutate func(*Execution)) (Execution, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Execution{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	exec := s.ensureExecutionLocked(id)
	prev := exec

	if exec.Permission == state && mutate == nil {
		return exec, false
	}

	exec.Permission = state
	if mutate != nil {
		mutate(&exec)
	}
	normalizeExecution(&exec)

	changed := !reflect.DeepEqual(exec, prev)
	if changed {
		s.entries[id] = exec
	}
	return exec, changed
}

func (s *Store) mutate(id string, mutate func(*Execution)) (Execution, bool) {
	id = strings.TrimSpace(id)
	if id == "" || mutate == nil {
		return Execution{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	exec := s.ensureExecutionLocked(id)
	prev := exec

	mutate(&exec)
	normalizeExecution(&exec)

	changed := !reflect.DeepEqual(exec, prev)
	if changed {
		s.entries[id] = exec
	}
	return exec, changed
}

func (s *Store) ensureExecutionLocked(id string) Execution {
	exec, ok := s.entries[id]
	if !ok {
		exec.ID = id
		exec.Call.ID = id
	}
	return exec
}
func mergeCall(prev, next tooltypes.Call) tooltypes.Call {
	if strings.TrimSpace(prev.ID) == "" {
		prev.ID = strings.TrimSpace(next.ID)
	}
	if strings.TrimSpace(next.Name) != "" {
		prev.Name = strings.TrimSpace(next.Name)
	}
	if strings.TrimSpace(next.Input) != "" {
		prev.Input = next.Input
	}
	if strings.TrimSpace(next.Reason) != "" {
		prev.Reason = next.Reason
	}
	prev.Finished = prev.Finished || next.Finished
	return prev
}
func mergePendingResult(exec Execution, next tooltypes.Result) tooltypes.Result {
	merged := next
	if strings.TrimSpace(merged.ToolCallID) == "" {
		merged.ToolCallID = exec.Call.ID
	}
	if strings.TrimSpace(merged.Name) == "" {
		merged.Name = exec.Call.Name
	}
	prev := exec.Result
	if strings.TrimSpace(prev.Metadata) != "" && strings.TrimSpace(merged.Metadata) == "" {
		merged.Metadata = prev.Metadata
	}
	if strings.TrimSpace(prev.Content) != "" && strings.TrimSpace(merged.Content) == "" {
		merged.Content = prev.Content
	}
	merged.IsError = merged.IsError || prev.IsError
	return merged
}
func mergeFinalResult(exec Execution, next tooltypes.Result) tooltypes.Result {
	merged := next
	if strings.TrimSpace(merged.ToolCallID) == "" {
		merged.ToolCallID = exec.Call.ID
	}
	if strings.TrimSpace(merged.Name) == "" {
		merged.Name = exec.Call.Name
	}
	merged.Metadata = MergeMetadata(exec.Result.Metadata, merged.Metadata)
	if strings.TrimSpace(merged.Content) == "" {
		merged.Content = exec.Result.Content
	}
	merged.IsError = merged.IsError || exec.Result.IsError
	return merged
}

// CanonicalToolName derives the display/tool registry name using the combined
// call and result context, normalising async aliases.
func CanonicalToolName(id, callName, resultName string, extras ...string) string {
	primary := []string{callName, resultName}
	metadataCandidates := make([]string, 0, len(extras))
	fallback := make([]string, 0, len(extras))
	for _, extra := range extras {
		if tool := toolNameFromMetadata(extra); tool != "" {
			metadataCandidates = append(metadataCandidates, tool)
			continue
		}
		fallback = append(fallback, extra)
	}
	ordered := [][]string{primary, metadataCandidates}
	for _, group := range ordered {
		for _, candidate := range group {
			trimmed := strings.TrimSpace(candidate)
			if trimmed == "" {
				continue
			}
			if strings.EqualFold(trimmed, tooling.AsyncToolName) {
				continue
			}
			return trimmed
		}
	}
	if looksAsync(id, callName, resultName, extras...) {
		return tooling.AsyncToolName
	}
	for _, candidate := range append(append(primary, metadataCandidates...), fallback...) {
		trimmed := strings.TrimSpace(candidate)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
func looksAsync(id, callName, resultName string, extras ...string) bool {
	if isAsyncID(id) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(callName), tooling.AsyncToolName) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(resultName), tooling.AsyncToolName) {
		return true
	}
	for _, extra := range extras {
		if hasAsyncMarker(extra) {
			return true
		}
		if tool := toolNameFromMetadata(extra); strings.EqualFold(tool, tooling.AsyncToolName) {
			return true
		}
	}
	return false
}
func hasAsyncMarker(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return false
	}
	return strings.Contains(trimmed, "\"async_task\"") || strings.Contains(trimmed, "async task") || strings.Contains(trimmed, strings.ToLower(tooling.AsyncToolName))
}
func toolNameFromMetadata(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var payload struct {
		AsyncTask struct {
			Tool string `json:"tool"`
		} `json:"async_task"`
		Task struct {
			Tool string `json:"tool"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return ""
	}
	if tool := strings.TrimSpace(payload.AsyncTask.Tool); tool != "" {
		return tool
	}
	return strings.TrimSpace(payload.Task.Tool)
}
func isAsyncID(id string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(id))
	return trimmed != "" && strings.HasPrefix(trimmed, "async_")
}
func normalizeExecution(exec *Execution) {
	if exec.Result.ToolCallID == "" {
	}
	metaExtras := exec.Result.Metadata
	extras := []string{exec.Call.Input, metaExtras, exec.Result.Content}
	if exec.Tool == "" {
	}
	if exec.Tool == "" {
	}
	if !exec.Flags.Async && looksAsync(exec.ID, exec.Call.Name, exec.Result.Name, extras...) {
	}
	if exec.Flags.Async {
		preferred := firstNonEmpty(exec.Result.Name, exec.Call.Name)
		if preserveAsyncTool(preferred) {
		} else {
		}
	}
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
func preserveAsyncTool(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	if strings.EqualFold(trimmed, tooling.AsyncToolName) {
		return false
	}
	if tooling.IsAgentCommandToolName(trimmed) {
		return true
	}
	if _, ok := toolregistry.Lookup(trimmed); ok {
		return true
	}
	return false
}
func deriveLifecycle(exec Execution) Lifecycle {
	switch exec.Lifecycle {
	case LifecycleFailed:
		return LifecycleFailed
	case LifecycleCompleted:
		if exec.Result.IsError {
			return LifecycleFailed
		}
		return LifecycleCompleted
	case LifecycleDeleted:
		return LifecycleDeleted
	}
	if exec.Call.Finished {
		if exec.Result.IsError {
			return LifecycleFailed
		}
		return LifecycleCompleted
	}
	if len(exec.Progress) > 0 {
		return LifecycleRunning
	}
	switch exec.Lifecycle {
	case LifecycleRunning:
		return LifecycleRunning
	case LifecyclePending:
		return LifecyclePending
	}
	if exec.ID != "" {
		return LifecyclePending
	}
	return LifecycleUnknown
}
func mergeFlags(prev, next ExecutionFlags) ExecutionFlags {
	if next.Async {
		prev.Async = true
	}
	if next.Persistent {
		prev.Persistent = true
	}
	if next.NeedsResume != prev.NeedsResume {
		prev.NeedsResume = next.NeedsResume
	}
	return prev
}
func mergeDisplay(prev, next ExecutionDisplay) ExecutionDisplay {
	if strings.TrimSpace(next.Label) != "" {
		prev.Label = strings.TrimSpace(next.Label)
	}
	if strings.TrimSpace(next.Summary) != "" {
		prev.Summary = strings.TrimSpace(next.Summary)
	}
	if len(next.Body) > 0 {
		prev.Body = append([]string(nil), next.Body...)
	}
	return prev
}
func copyProgressRecords(entries []ProgressRecord) []ProgressRecord {
	if len(entries) == 0 {
		return nil
	}
	out := make([]ProgressRecord, len(entries))
	copy(out, entries)
	return out
}
