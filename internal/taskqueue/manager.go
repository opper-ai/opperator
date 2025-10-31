package taskqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusLoading  Status = "loading"
	StatusPending  Status = "pending"
	StatusComplete Status = "complete"
	StatusFailed   Status = "failed"
)

// Task captures the persisted state for an asynchronous tool execution.
type Task struct {
	ID          string          `json:"id"`
	ToolName    string          `json:"tool_name"`
	Args        string          `json:"args"`
	WorkingDir  string          `json:"working_dir"`
	SessionID   string          `json:"session_id,omitempty"`
	CallID      string          `json:"call_id,omitempty"`
	Mode        string          `json:"mode,omitempty"`
	AgentName   string          `json:"agent_name,omitempty"`
	CommandName string          `json:"command_name,omitempty"`
	CommandArgs string          `json:"command_args,omitempty"`
	Origin      string          `json:"origin,omitempty"`
	ClientID    string          `json:"client_id,omitempty"`
	Status      Status          `json:"status"`
	Result      string          `json:"result,omitempty"`
	Metadata    string          `json:"metadata,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	Progress    []ProgressEntry `json:"progress,omitempty"`
}

// ProgressEntry captures a single progress update emitted by a task.
type ProgressEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Text      string    `json:"text,omitempty"`
	Metadata  string    `json:"metadata,omitempty"`
	Status    string    `json:"status,omitempty"`
}

// ManagerOptions configures the task queue manager behaviour.
type ManagerOptions struct {
	WorkerCount          int
	QueueSize            int
	MaxPendingPerSession int
}

type MetricsSnapshot struct {
	Submitted   int64
	InFlight    int64
	Completed   int64
	Failed      int64
	QueueDepth  int64
	WorkerCount int64
}

type metrics struct {
	submitted atomic.Int64
	inFlight  atomic.Int64
	completed atomic.Int64
	failed    atomic.Int64
}

type progressRequest struct {
	taskID string
	entry  ProgressEntry
	done   chan error
}

type TaskEventType string

const (
	TaskEventSnapshot  TaskEventType = "snapshot"
	TaskEventProgress  TaskEventType = "progress"
	TaskEventCompleted TaskEventType = "completed"
	TaskEventFailed    TaskEventType = "failed"
	TaskEventDeleted   TaskEventType = "deleted"
)

type TaskEvent struct {
	Type     TaskEventType  `json:"type"`
	Task     *Task          `json:"task,omitempty"`
	Progress *ProgressEntry `json:"progress,omitempty"`
	Error    string         `json:"error,omitempty"`
}

type taskWatcher struct {
	ch   chan TaskEvent
	once sync.Once
}

func newTaskWatcher(buffer int) *taskWatcher {
	if buffer <= 0 {
		buffer = 16
	}
	return &taskWatcher{ch: make(chan TaskEvent, buffer)}
}

func (w *taskWatcher) send(ev TaskEvent) {
	if w == nil {
		return
	}
	select {
	case w.ch <- ev:
	default:
	}
}

func (w *taskWatcher) close() {
	if w == nil {
		return
	}
	w.once.Do(func() { close(w.ch) })
}

func newMetrics() *metrics {
	return &metrics{}
}

func normalizeOptions(opts *ManagerOptions) ManagerOptions {
	defaults := ManagerOptions{
		WorkerCount:          runtime.NumCPU(),
		QueueSize:            32,
		MaxPendingPerSession: defaultMaxPendingPerSession,
	}
	if defaults.WorkerCount < 1 {
		defaults.WorkerCount = 1
	}
	if opts == nil {
		return defaults
	}
	if opts.WorkerCount > 0 {
		defaults.WorkerCount = opts.WorkerCount
	}
	if defaults.WorkerCount < 1 {
		defaults.WorkerCount = 1
	}
	if opts.QueueSize > 0 {
		defaults.QueueSize = opts.QueueSize
	}
	if defaults.QueueSize < 1 {
		defaults.QueueSize = 1
	}
	if opts.MaxPendingPerSession >= 0 {
		defaults.MaxPendingPerSession = opts.MaxPendingPerSession
	}
	return defaults
}

func (m *metrics) snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	return MetricsSnapshot{
		Submitted: m.submitted.Load(),
		InFlight:  m.inFlight.Load(),
		Completed: m.completed.Load(),
		Failed:    m.failed.Load(),
	}
}

func (m *Manager) logTaskEvent(action string, task *Task, duration time.Duration, err error) {
	if task == nil {
		return
	}
	record := map[string]any{
		"event":        strings.TrimSpace(action),
		"task_id":      strings.TrimSpace(task.ID),
		"tool":         strings.TrimSpace(task.ToolName),
		"mode":         strings.TrimSpace(task.Mode),
		"session_id":   strings.TrimSpace(task.SessionID),
		"call_id":      strings.TrimSpace(task.CallID),
		"origin":       strings.TrimSpace(task.Origin),
		"client_id":    strings.TrimSpace(task.ClientID),
		"status":       strings.TrimSpace(string(task.Status)),
		"submitted_at": task.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if duration > 0 {
		record["duration_ms"] = duration.Milliseconds()
	}
	if task.UpdatedAt.After(task.CreatedAt) {
		record["updated_at"] = task.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if task.CompletedAt != nil && !task.CompletedAt.IsZero() {
		record["completed_at"] = task.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	if err != nil {
		record["error"] = strings.TrimSpace(err.Error())
	}
	if b, marshalErr := json.Marshal(record); marshalErr == nil {
		log.Printf("taskqueue:event %s", string(b))
	} else {
		log.Printf("taskqueue:event %s task=%s err=%v", action, task.ID, err)
	}
}

func (t *Task) Clone() *Task {
	if t == nil {
		return nil
	}
	clone := *t
	if len(t.Progress) > 0 {
		clone.Progress = make([]ProgressEntry, len(t.Progress))
		copy(clone.Progress, t.Progress)
	}
	return &clone
}

type ToolRunner interface {
	Execute(ctx context.Context, name, args, workingDir string) (content string, metadata string, err error)
}

type ProgressEvent struct {
	Text     string
	Metadata string
	Status   string
}

// AgentRunner executes agent commands asynchronously while emitting progress.
type AgentRunner interface {
	Execute(ctx context.Context, agent, command, args, workingDir string, progress func(ProgressEvent)) (content string, metadata string, err error)
}

type SubmitRequest struct {
	ToolName    string
	Args        string
	WorkingDir  string
	SessionID   string
	CallID      string
	Mode        string
	AgentName   string
	Command     string
	CommandArgs string
	Origin      string
	ClientID    string
}

// Manager coordinates asynchronous tool tasks, persisting their state and
// orchestrating execution via a worker pool.
type Manager struct {
	mu                   sync.RWMutex
	tasks                map[string]*Task
	queue                chan string
	runner               ToolRunner
	agent                AgentRunner
	db                   *sql.DB
	discarded            map[string]struct{}
	cancels              map[string]context.CancelFunc
	ctx                  context.Context
	cancel               context.CancelFunc
	queueSize            int
	workerCount          int
	maxPendingPerSession int
	metrics              *metrics
	watchMu              sync.RWMutex
	watchers             map[string]map[*taskWatcher]struct{}
	progressQueue        chan progressRequest
	wg                   sync.WaitGroup
	eventMu              sync.RWMutex
	eventSink            func(TaskEvent)
}

// ErrClosed indicates the manager has been shut down and cannot accept work.
var ErrClosed = errors.New("task queue closed")

// NewManager constructs a manager backed by the provided storage path and
// execution runner. In-flight tasks persisted with loading/pending states are
// automatically resumed.
const defaultMaxPendingPerSession = 20
const defaultTaskOrigin = "unknown"

func NewManager(ctx context.Context, db *sql.DB, runner ToolRunner, agent AgentRunner) (*Manager, error) {
	return NewManagerWithOptions(ctx, db, runner, agent, nil)
}

func NewManagerWithOptions(ctx context.Context, db *sql.DB, runner ToolRunner, agent AgentRunner, opts *ManagerOptions) (*Manager, error) {
	if runner == nil {
		return nil, fmt.Errorf("tool runner is required")
	}
	if db == nil {
		return nil, fmt.Errorf("database handle is required")
	}
	options := normalizeOptions(opts)
	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	queueCtx, cancel := context.WithCancel(baseCtx)
	mgr := &Manager{
		tasks:                make(map[string]*Task),
		queue:                make(chan string, options.QueueSize),
		runner:               runner,
		agent:                agent,
		db:                   db,
		discarded:            make(map[string]struct{}),
		cancels:              make(map[string]context.CancelFunc),
		ctx:                  queueCtx,
		cancel:               cancel,
		queueSize:            options.QueueSize,
		workerCount:          options.WorkerCount,
		maxPendingPerSession: options.MaxPendingPerSession,
		metrics:              newMetrics(),
		watchers:             make(map[string]map[*taskWatcher]struct{}),
		progressQueue:        make(chan progressRequest, 64),
	}
	if err := mgr.loadFromDatabase(); err != nil {
		cancel()
		return nil, err
	}
	mgr.resumeIncomplete()
	mgr.startWorkers(options.WorkerCount)
	mgr.wg.Add(1)
	go mgr.progressWriter()
	return mgr, nil
}

// SetEventSink registers a callback that receives every task event emitted by
// the manager. The sink must be non-blocking; events are delivered on the
// caller's goroutine with best-effort semantics.
func (m *Manager) SetEventSink(sink func(TaskEvent)) {
	if m == nil {
		return
	}
	m.eventMu.Lock()
	m.eventSink = sink
	m.eventMu.Unlock()
}

func (m *Manager) emitTaskEvent(event TaskEvent) {
	if m == nil {
		return
	}
	m.eventMu.RLock()
	sink := m.eventSink
	m.eventMu.RUnlock()
	if sink == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("taskqueue: task event sink panic recovered: %v", r)
		}
	}()
	sink(event)
}

// Submit enqueues a new asynchronous tool task for execution.
func (m *Manager) Submit(ctx context.Context, req SubmitRequest) (*Task, error) {
	if m == nil {
		return nil, fmt.Errorf("task manager not initialised")
	}
	m.mu.RLock()
	closed := m.ctx.Err()
	m.mu.RUnlock()
	if closed != nil {
		return nil, ErrClosed
	}
	name := strings.TrimSpace(req.ToolName)
	if name == "" {
		return nil, fmt.Errorf("tool name is required")
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "tool"
	}
	if mode == "agent" && m.agent == nil {
		return nil, fmt.Errorf("agent runner is not configured")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	origin := strings.TrimSpace(req.Origin)
	clientID := strings.TrimSpace(req.ClientID)
	if origin == "" {
		origin = defaultTaskOrigin
	}
	if limit := m.pendingLimit(); limit > 0 {
		normalised := normaliseSessionForLimit(sessionID)
		m.mu.RLock()
		pending := m.countPendingLocked(normalised)
		m.mu.RUnlock()
		if pending >= limit {
			return nil, fmt.Errorf("pending async task limit reached for session %s (limit %d)", friendlySessionLabel(sessionID), limit)
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC()
	task := &Task{
		ID:          uuid.NewString(),
		ToolName:    name,
		Args:        req.Args,
		WorkingDir:  strings.TrimSpace(req.WorkingDir),
		SessionID:   sessionID,
		CallID:      strings.TrimSpace(req.CallID),
		Mode:        mode,
		AgentName:   strings.TrimSpace(req.AgentName),
		CommandName: strings.TrimSpace(req.Command),
		CommandArgs: req.CommandArgs,
		Origin:      origin,
		ClientID:    clientID,
		Status:      StatusLoading,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.mu.Lock()
	m.tasks[task.ID] = task
	if err := m.saveTaskLocked(task); err != nil {
		delete(m.tasks, task.ID)
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()

	m.emitTaskEvent(TaskEvent{Type: TaskEventSnapshot, Task: task.Clone()})

	select {
	case m.queue <- task.ID:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.ctx.Done():
		return nil, ErrClosed
	}

	if m.metrics != nil {
		m.metrics.submitted.Add(1)
	}

	return task.Clone(), nil
}

func (m *Manager) pendingLimit() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxPendingPerSession
}

// SetMaxPendingPerSession overrides the default per-session submission cap. A value
func (m *Manager) SetMaxPendingPerSession(limit int) {
	if m == nil {
		return
	}
	if limit < 0 {
		limit = 0
	}
	m.mu.Lock()
	m.maxPendingPerSession = limit
	m.mu.Unlock()
}

func (m *Manager) MetricsSnapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	snapshot := MetricsSnapshot{}
	if m.metrics != nil {
		snapshot = m.metrics.snapshot()
	}
	snapshot.QueueDepth = int64(len(m.queue))
	snapshot.WorkerCount = int64(m.WorkerCount())
	return snapshot
}

func (m *Manager) WorkerCount() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.workerCount
}

// SubscribeTask registers a watcher for task updates. The caller receives an initial
// snapshot followed by progress/terminal events. The returned cancel function must be
// invoked to release resources.
func (m *Manager) SubscribeTask(taskID string) (<-chan TaskEvent, func(), error) {
	if m == nil {
		return nil, nil, fmt.Errorf("task manager not initialised")
	}
	trimmed := strings.TrimSpace(taskID)
	if trimmed == "" {
		return nil, nil, fmt.Errorf("task id is required")
	}
	watcher := newTaskWatcher(32)
	m.watchMu.Lock()
	set := m.watchers[trimmed]
	if set == nil {
		set = make(map[*taskWatcher]struct{})
		m.watchers[trimmed] = set
	}
	set[watcher] = struct{}{}
	m.watchMu.Unlock()

	m.mu.RLock()
	if task, ok := m.tasks[trimmed]; ok && task != nil {
		watcher.send(TaskEvent{Type: TaskEventSnapshot, Task: task.Clone()})
	}
	m.mu.RUnlock()

	cancelOnce := sync.Once{}
	cancel := func() {
		cancelOnce.Do(func() {
			m.removeWatcher(trimmed, watcher)
			watcher.close()
		})
	}

	return watcher.ch, cancel, nil
}

func (m *Manager) removeWatcher(taskID string, watcher *taskWatcher) {
	if m == nil || watcher == nil {
		return
	}
	m.watchMu.Lock()
	defer m.watchMu.Unlock()
	if set, ok := m.watchers[taskID]; ok {
		delete(set, watcher)
		if len(set) == 0 {
			delete(m.watchers, taskID)
		}
	}
}

func (m *Manager) copyWatchers(taskID string) []*taskWatcher {
	m.watchMu.RLock()
	defer m.watchMu.RUnlock()
	set := m.watchers[taskID]
	if len(set) == 0 {
		return nil
	}
	result := make([]*taskWatcher, 0, len(set))
	for watcher := range set {
		result = append(result, watcher)
	}
	return result
}

func (m *Manager) detachWatchers(taskID string) []*taskWatcher {
	m.watchMu.Lock()
	defer m.watchMu.Unlock()
	set := m.watchers[taskID]
	if len(set) == 0 {
		delete(m.watchers, taskID)
		return nil
	}
	result := make([]*taskWatcher, 0, len(set))
	for watcher := range set {
		result = append(result, watcher)
	}
	delete(m.watchers, taskID)
	return result
}

func (m *Manager) broadcastTaskEvent(taskID string, event TaskEvent) {
	watchers := m.copyWatchers(taskID)
	if len(watchers) == 0 {
		m.emitTaskEvent(event)
		return
	}
	for _, watcher := range watchers {
		watcher.send(event)
	}
	m.emitTaskEvent(event)
}

func (m *Manager) finishWatchers(taskID string, event TaskEvent) {
	watchers := m.detachWatchers(taskID)
	if len(watchers) == 0 {
		m.emitTaskEvent(event)
		return
	}
	for _, watcher := range watchers {
		watcher.send(event)
		watcher.close()
	}
	m.emitTaskEvent(event)
}

func (m *Manager) Get(id string) (*Task, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[strings.TrimSpace(id)]
	if !ok {
		return nil, false
	}
	return task.Clone(), true
}

func (m *Manager) List() []*Task {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.tasks) == 0 {
		return nil
	}
	result := make([]*Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		result = append(result, task.Clone())
	}
	return result
}

// ActiveTasks returns a snapshot of tasks that are still in-flight.
func (m *Manager) ActiveTasks() []*Task {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.tasks) == 0 {
		return nil
	}
	result := make([]*Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		if task == nil {
			continue
		}
		switch task.Status {
		case StatusLoading, StatusPending:
			result = append(result, task.Clone())
		}
	}
	return result
}

// Shutdown stops the worker and waits for it to finish.
func (m *Manager) Shutdown() {
	if m == nil {
		return
	}
	m.cancel()
	m.wg.Wait()
}

func (m *Manager) progressWriter() {
	defer m.wg.Done()
	if m == nil {
		return
	}
	for {
		select {
		case <-m.ctx.Done():
			return
		case req, ok := <-m.progressQueue:
			if !ok {
				return
			}
			var err error
			if trimmed := strings.TrimSpace(req.taskID); trimmed != "" {
				err = m.insertProgressLocked(trimmed, req.entry)
			}
			if req.done != nil {
				req.done <- err
			}
		}
	}
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case id, ok := <-m.queue:
			if !ok {
				return
			}
			m.runSafe(id)
		}
	}
}

func (m *Manager) startWorkers(count int) {
	if count < 1 {
		count = 1
	}
	for i := 0; i < count; i++ {
		m.wg.Add(1)
		go m.worker()
	}
}

func (m *Manager) runSafe(id string) {
	trimmedID := strings.TrimSpace(id)
	executed := false
	var runErr error
	if m.metrics != nil {
		m.metrics.inFlight.Add(1)
	}
	defer func() {
		if r := recover(); r != nil {
			runErr = fmt.Errorf("panic: %v", r)
			log.Printf("taskqueue: recovered panic while executing task %s: %v", trimmedID, runErr)
			m.failTaskAfterPanic(trimmedID, runErr)
			executed = true
		}
		if m.metrics != nil {
			if executed {
				if runErr != nil {
					m.metrics.failed.Add(1)
				} else {
					m.metrics.completed.Add(1)
				}
			}
			m.metrics.inFlight.Add(-1)
		}
	}()
	executed, runErr = m.run(trimmedID)
}

func (m *Manager) failTaskAfterPanic(id string, panicErr error) {
	if id == "" {
		return
	}
	m.mu.Lock()
	task, ok := m.tasks[id]
	if !ok || task == nil {
		m.mu.Unlock()
		return
	}
	if _, removed := m.discarded[id]; removed {
		m.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	task.Status = StatusFailed
	panicMsg := panicErr.Error()
	if strings.TrimSpace(task.Error) != "" {
		panicMsg = fmt.Sprintf("%s; previous error: %s", panicMsg, task.Error)
	}
	task.Error = panicMsg
	task.Result = ""
	task.Metadata = mergeProgressMetadata(task.Metadata, task.Progress)
	task.CompletedAt = &now
	task.UpdatedAt = now
	taskClone := task.Clone()
	if err := m.saveTaskLocked(task); err != nil {
		log.Printf("taskqueue: save panic failure for task %s: %v", id, err)
	}
	m.mu.Unlock()
	m.logTaskEvent("failed", taskClone, 0, panicErr)
	m.finishWatchers(id, TaskEvent{Type: TaskEventFailed, Task: taskClone, Error: panicMsg})
}

func (m *Manager) run(id string) (bool, error) {
	m.mu.Lock()
	task, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return false, nil
	}
	if _, removed := m.discarded[id]; removed {
		m.mu.Unlock()
		return false, nil
	}
	start := time.Now()
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancels[id] = cancel
	task.Status = StatusPending
	task.Error = ""
	task.Result = ""
	task.Metadata = ""
	task.CompletedAt = nil
	task.UpdatedAt = time.Now().UTC()
	taskSnapshot := task.Clone()
	if err := m.saveTaskLocked(task); err != nil {
		log.Printf("taskqueue: save pending state for task %s: %v", id, err)
	}
	m.mu.Unlock()
	m.logTaskEvent("started", taskSnapshot, 0, nil)
	m.emitTaskEvent(TaskEvent{Type: TaskEventSnapshot, Task: taskSnapshot})

	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.cancels, id)
		m.mu.Unlock()
	}()
	var (
		content  string
		metadata string
		err      error
	)
	if strings.EqualFold(task.Mode, "agent") {
		if m.agent == nil {
			err = fmt.Errorf("agent runner not configured")
		} else {
			progress := func(ev ProgressEvent) {
				m.appendProgress(task.ID, ev)
			}
			content, metadata, err = m.agent.Execute(ctx, task.AgentName, task.CommandName, task.CommandArgs, task.WorkingDir, progress)
		}
	} else {
		content, metadata, err = m.runner.Execute(ctx, task.ToolName, task.Args, task.WorkingDir)
	}
	m.mu.Lock()
	now := time.Now().UTC()
	task.UpdatedAt = now
	if err != nil {
		task.Status = StatusFailed
		task.Error = strings.TrimSpace(err.Error())
		task.Result = ""
		task.Metadata = mergeProgressMetadata("", task.Progress)
	} else {
		task.Status = StatusComplete
		task.Result = content
		task.Metadata = mergeProgressMetadata(metadata, task.Progress)
		task.Error = ""
	}
	task.CompletedAt = &now
	taskClone := task.Clone()
	if err := m.saveTaskLocked(task); err != nil {
		log.Printf("taskqueue: save terminal state for task %s: %v", id, err)
	}
	m.mu.Unlock()

	duration := time.Since(start)
	if err != nil {
		m.logTaskEvent("failed", taskClone, duration, err)
	} else {
		m.logTaskEvent("completed", taskClone, duration, nil)
	}
	eventType := TaskEventCompleted
	errMsg := ""
	if err != nil {
		eventType = TaskEventFailed
		errMsg = strings.TrimSpace(err.Error())
	}
	m.finishWatchers(id, TaskEvent{Type: eventType, Task: taskClone, Error: errMsg})

	return true, err
}

func (m *Manager) appendProgress(taskID string, event ProgressEvent) {
	trimmedID := strings.TrimSpace(taskID)
	if trimmedID == "" {
		return
	}
	entry := ProgressEntry{
		Timestamp: time.Now().UTC(),
		Text:      strings.TrimSpace(event.Text),
		Metadata:  strings.TrimSpace(event.Metadata),
		Status:    strings.TrimSpace(event.Status),
	}
	var notify bool
	var payload TaskEvent
	m.mu.Lock()
	task, ok := m.tasks[trimmedID]
	if ok && task != nil {
		if _, removed := m.discarded[trimmedID]; removed {
			m.mu.Unlock()
			return
		}
		hasPayload := entry.Text != "" || entry.Metadata != "" || entry.Status != ""
		if hasPayload {
			task.Progress = append(task.Progress, entry)
			if len(task.Progress) > 200 {
				task.Progress = task.Progress[len(task.Progress)-200:]
			}
		}
		task.Metadata = mergeProgressMetadata(task.Metadata, task.Progress)
		task.UpdatedAt = entry.Timestamp
		if err := m.saveTaskLocked(task); err != nil {
			log.Printf("taskqueue: save progress metadata for task %s: %v", trimmedID, err)
		}
		if hasPayload {
			notify = true
			taskClone := task.Clone()
			entryCopy := entry
			payload = TaskEvent{Type: TaskEventProgress, Task: taskClone, Progress: &entryCopy}
		}
	}
	m.mu.Unlock()
	if notify {
		m.broadcastTaskEvent(trimmedID, payload)
	}
	if notify {
		done := make(chan error, 1)
		select {
		case m.progressQueue <- progressRequest{taskID: trimmedID, entry: entry, done: done}:
		case <-m.ctx.Done():
			return
		}
		select {
		case err := <-done:
			if err != nil {
				log.Printf("taskqueue: insert progress for task %s: %v", trimmedID, err)
			}
		case <-time.After(2 * time.Second):
			log.Printf("taskqueue: insert progress for task %s timed out", trimmedID)
		}
	}
}

const globalSessionKey = "__global__"

func normaliseSessionForLimit(sessionID string) string {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return globalSessionKey
	}
	return trimmed
}

func friendlySessionLabel(sessionID string) string {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return "global"
	}
	return trimmed
}

func (m *Manager) countPendingLocked(sessionKey string) int {
	if m == nil || sessionKey == "" {
		return 0
	}
	count := 0
	for _, task := range m.tasks {
		if task == nil {
			continue
		}
		if normaliseSessionForLimit(task.SessionID) != sessionKey {
			continue
		}
		switch task.Status {
		case StatusLoading, StatusPending:
			count++
		}
	}
	return count
}

func mergeProgressMetadata(base string, progress []ProgressEntry) string {
	trimmed := strings.TrimSpace(base)
	if trimmed == "" && len(progress) == 0 {
		return ""
	}
	var meta map[string]any
	if trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &meta); err != nil || meta == nil {
			meta = map[string]any{"original_metadata": trimmed}
		}
	}
	if meta == nil {
		meta = make(map[string]any)
	}
	if len(progress) > 0 {
		last := progress[len(progress)-1]
		meta["progress_count"] = len(progress)
		if !last.Timestamp.IsZero() {
			meta["last_progress_ts"] = last.Timestamp.Format(time.RFC3339)
		}
		if text := strings.TrimSpace(last.Text); text != "" {
			meta["last_progress_text"] = text
		}
		if status := strings.TrimSpace(last.Status); status != "" {
			meta["last_progress_status"] = status
		}
		if md := strings.TrimSpace(last.Metadata); md != "" {
			meta["last_progress_metadata"] = md
		}
	}
	if len(meta) == 0 {
		return ""
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return trimmed
	}
	return string(b)
}

func (m *Manager) saveTaskLocked(task *Task) error {
	if m == nil || m.db == nil || task == nil {
		return nil
	}
	id := strings.TrimSpace(task.ID)
	if id == "" {
		return fmt.Errorf("task id is required")
	}
	if _, removed := m.discarded[id]; removed {
		delete(m.discarded, id)
		return nil
	}
	created := task.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
		task.CreatedAt = created
	}
	updated := task.UpdatedAt
	if updated.IsZero() {
		updated = created
		task.UpdatedAt = updated
	}
	completed := interface{}(nil)
	if task.CompletedAt != nil && !task.CompletedAt.IsZero() {
		completed = task.CompletedAt.UTC().UnixNano()
	}
	sessionArg := interface{}(nil)
	if trimmedSession := strings.TrimSpace(task.SessionID); trimmedSession != "" {
		sessionArg = trimmedSession
	}
	callArg := interface{}(nil)
	if trimmedCall := strings.TrimSpace(task.CallID); trimmedCall != "" {
		callArg = trimmedCall
	}
	modeValue := strings.TrimSpace(task.Mode)
	if modeValue == "" {
		modeValue = "tool"
	}
	statusValue := strings.TrimSpace(string(task.Status))
	if statusValue == "" {
		statusValue = string(StatusPending)
	}
	originValue := strings.TrimSpace(task.Origin)
	clientValue := strings.TrimSpace(task.ClientID)
	_, err := m.db.ExecContext(
		context.Background(),
		`INSERT INTO tool_tasks (
			id, tool_name, args, working_dir, session_id, call_id, mode, agent_name,
			command_name, command_args, origin, client_id, status, result, metadata, error,
			created_at, updated_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			tool_name = excluded.tool_name,
			args = excluded.args,
			working_dir = excluded.working_dir,
			session_id = excluded.session_id,
			call_id = excluded.call_id,
			mode = excluded.mode,
			agent_name = excluded.agent_name,
			command_name = excluded.command_name,
			command_args = excluded.command_args,
			origin = excluded.origin,
			client_id = excluded.client_id,
			status = excluded.status,
			result = excluded.result,
			metadata = excluded.metadata,
			error = excluded.error,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at,
			completed_at = excluded.completed_at`,
		id,
		strings.TrimSpace(task.ToolName),
		strings.TrimSpace(task.Args),
		strings.TrimSpace(task.WorkingDir),
		sessionArg,
		callArg,
		modeValue,
		strings.TrimSpace(task.AgentName),
		strings.TrimSpace(task.CommandName),
		strings.TrimSpace(task.CommandArgs),
		originValue,
		clientValue,
		statusValue,
		strings.TrimSpace(task.Result),
		strings.TrimSpace(task.Metadata),
		strings.TrimSpace(task.Error),
		created.UTC().UnixNano(),
		updated.UTC().UnixNano(),
		completed,
	)
	return err
}

func (m *Manager) insertProgressLocked(taskID string, entry ProgressEntry) error {
	if m == nil || m.db == nil {
		return nil
	}
	id := strings.TrimSpace(taskID)
	if id == "" {
		return nil
	}
	ts := entry.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	_, err := m.db.ExecContext(
		context.Background(),
		`INSERT INTO tool_task_progress (task_id, timestamp, text, metadata, status) VALUES (?, ?, ?, ?, ?)`,
		id,
		ts.UTC().UnixNano(),
		strings.TrimSpace(entry.Text),
		strings.TrimSpace(entry.Metadata),
		strings.TrimSpace(entry.Status),
	)
	return err
}

func (m *Manager) replaceProgressLocked(taskID string, entries []ProgressEntry) error {
	if m == nil || m.db == nil {
		return nil
	}
	id := strings.TrimSpace(taskID)
	if id == "" {
		return nil
	}
	tx, err := m.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.Exec(`DELETE FROM tool_task_progress WHERE task_id = ?`, id); err != nil {
		return err
	}
	for _, entry := range entries {
		ts := entry.Timestamp
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		if _, err = tx.Exec(
			`INSERT INTO tool_task_progress (task_id, timestamp, text, metadata, status) VALUES (?, ?, ?, ?, ?)`,
			id,
			ts.UTC().UnixNano(),
			strings.TrimSpace(entry.Text),
			strings.TrimSpace(entry.Metadata),
			strings.TrimSpace(entry.Status),
		); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (m *Manager) cancelTaskLocked(taskID string) context.CancelFunc {
	if m == nil {
		return nil
	}
	if cancel, ok := m.cancels[taskID]; ok {
		delete(m.cancels, taskID)
		return cancel
	}
	return nil
}

func (m *Manager) pruneOrphanedProgress(ctx context.Context) {
	if m == nil || m.db == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_, _ = m.db.ExecContext(ctx, `DELETE FROM tool_task_progress WHERE task_id NOT IN (SELECT id FROM tool_tasks)`)
}

func (m *Manager) DeleteTask(ctx context.Context, taskID string) (bool, error) {
	if m == nil || m.db == nil {
		return false, fmt.Errorf("task manager not initialised")
	}
	id := strings.TrimSpace(taskID)
	if id == "" {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	var cancel context.CancelFunc
	_, existed := m.tasks[id]
	if existed {
		delete(m.tasks, id)
		m.discarded[id] = struct{}{}
		cancel = m.cancelTaskLocked(id)
	}
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return existed, err
	}
	if _, err := tx.Exec(`DELETE FROM tool_task_progress WHERE task_id = ?`, id); err != nil {
		_ = tx.Rollback()
		return existed, err
	}
	if _, err := tx.Exec(`DELETE FROM tool_tasks WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return existed, err
	}
	if err := tx.Commit(); err != nil {
		return existed, err
	}
	m.pruneOrphanedProgress(ctx)
	m.finishWatchers(id, TaskEvent{Type: TaskEventDeleted})
	return existed, nil
}

func (m *Manager) DeleteTasksBySession(ctx context.Context, sessionID string) (int, error) {
	if m == nil || m.db == nil {
		return 0, fmt.Errorf("task manager not initialised")
	}
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	var cancels []context.CancelFunc
	var removedIDs []string
	for id, task := range m.tasks {
		if task == nil {
			continue
		}
		if strings.TrimSpace(task.SessionID) != trimmed {
			continue
		}
		delete(m.tasks, id)
		m.discarded[id] = struct{}{}
		if cancel := m.cancelTaskLocked(id); cancel != nil {
			cancels = append(cancels, cancel)
		}
		removedIDs = append(removedIDs, id)
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM tool_task_progress WHERE task_id IN (SELECT id FROM tool_tasks WHERE session_id = ?)`, trimmed); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	res, err := tx.Exec(`DELETE FROM tool_tasks WHERE session_id = ?`, trimmed)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	m.pruneOrphanedProgress(ctx)
	rows, _ := res.RowsAffected()
	for _, id := range removedIDs {
		m.finishWatchers(id, TaskEvent{Type: TaskEventDeleted})
	}
	return int(rows), nil
}

func (m *Manager) DeleteTasksByCall(ctx context.Context, callID string) (int, error) {
	if m == nil || m.db == nil {
		return 0, fmt.Errorf("task manager not initialised")
	}
	trimmed := strings.TrimSpace(callID)
	if trimmed == "" {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	var cancels []context.CancelFunc
	var removedIDs []string
	for id, task := range m.tasks {
		if task == nil {
			continue
		}
		if strings.TrimSpace(task.CallID) != trimmed {
			continue
		}
		delete(m.tasks, id)
		m.discarded[id] = struct{}{}
		if cancel := m.cancelTaskLocked(id); cancel != nil {
			cancels = append(cancels, cancel)
		}
		removedIDs = append(removedIDs, id)
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM tool_task_progress WHERE task_id IN (SELECT id FROM tool_tasks WHERE call_id = ?)`, trimmed); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	res, err := tx.Exec(`DELETE FROM tool_tasks WHERE call_id = ?`, trimmed)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	m.pruneOrphanedProgress(ctx)
	rows, _ := res.RowsAffected()
	for _, id := range removedIDs {
		m.finishWatchers(id, TaskEvent{Type: TaskEventDeleted})
	}
	return int(rows), nil
}

func (m *Manager) DeleteTasksByAgent(ctx context.Context, agentName string) (int, error) {
	if m == nil || m.db == nil {
		return 0, fmt.Errorf("task manager not initialised")
	}
	trimmed := strings.TrimSpace(agentName)
	if trimmed == "" {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	var cancels []context.CancelFunc
	var removedIDs []string
	for id, task := range m.tasks {
		if task == nil {
			continue
		}
		if strings.TrimSpace(task.AgentName) != trimmed {
			continue
		}
		delete(m.tasks, id)
		m.discarded[id] = struct{}{}
		if cancel := m.cancelTaskLocked(id); cancel != nil {
			cancels = append(cancels, cancel)
		}
		removedIDs = append(removedIDs, id)
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM tool_task_progress WHERE task_id IN (SELECT id FROM tool_tasks WHERE agent_name = ?)`, trimmed); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	res, err := tx.Exec(`DELETE FROM tool_tasks WHERE agent_name = ?`, trimmed)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	m.pruneOrphanedProgress(ctx)
	rows, _ := res.RowsAffected()
	for _, id := range removedIDs {
		m.finishWatchers(id, TaskEvent{Type: TaskEventDeleted})
	}
	return int(rows), nil
}

func (m *Manager) loadFromDatabase() error {
	if m == nil || m.db == nil {
		return fmt.Errorf("database handle is required")
	}
	rows, err := m.db.QueryContext(context.Background(), `
		SELECT
			id, tool_name, args, working_dir, session_id, call_id, mode, agent_name,
			command_name, command_args, origin, client_id, status, result, metadata, error,
			created_at, updated_at, completed_at
		FROM tool_tasks
	`)
	if err != nil {
		return fmt.Errorf("load tool tasks: %w", err)
	}
	defer rows.Close()
	tasks := make(map[string]*Task)
	for rows.Next() {
		var (
			id          string
			toolName    string
			args        sql.NullString
			workingDir  sql.NullString
			sessionID   sql.NullString
			callID      sql.NullString
			mode        sql.NullString
			agentName   sql.NullString
			commandName sql.NullString
			commandArgs sql.NullString
			origin      sql.NullString
			clientID    sql.NullString
			status      sql.NullString
			result      sql.NullString
			metadata    sql.NullString
			errorText   sql.NullString
			createdAt   int64
			updatedAt   int64
			completedAt sql.NullInt64
		)
		if err := rows.Scan(
			&id, &toolName, &args, &workingDir, &sessionID, &callID, &mode,
			&agentName, &commandName, &commandArgs, &origin, &clientID, &status, &result, &metadata,
			&errorText, &createdAt, &updatedAt, &completedAt,
		); err != nil {
			return fmt.Errorf("scan tool tasks: %w", err)
		}
		statusVal := strings.TrimSpace(status.String)
		if statusVal == "" {
			statusVal = string(StatusPending)
		}
		task := &Task{
			ID:         strings.TrimSpace(id),
			ToolName:   strings.TrimSpace(toolName),
			Args:       strings.TrimSpace(args.String),
			WorkingDir: strings.TrimSpace(workingDir.String),
			SessionID:  strings.TrimSpace(sessionID.String),
			CallID:     strings.TrimSpace(callID.String),
			Mode: func() string {
				if strings.TrimSpace(mode.String) == "" {
					return "tool"
				}
				return strings.TrimSpace(mode.String)
			}(),
			AgentName:   strings.TrimSpace(agentName.String),
			CommandName: strings.TrimSpace(commandName.String),
			CommandArgs: strings.TrimSpace(commandArgs.String),
			Origin:      strings.TrimSpace(origin.String),
			ClientID:    strings.TrimSpace(clientID.String),
			Result:      strings.TrimSpace(result.String),
			Metadata:    strings.TrimSpace(metadata.String),
			Error:       strings.TrimSpace(errorText.String),
			CreatedAt:   time.Unix(0, createdAt).UTC(),
			UpdatedAt:   time.Unix(0, updatedAt).UTC(),
		}
		task.Status = Status(statusVal)
		if completedAt.Valid {
			ts := time.Unix(0, completedAt.Int64).UTC()
			task.CompletedAt = &ts
		}
		tasks[task.ID] = task
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate tool tasks: %w", err)
	}
	progRows, err := m.db.QueryContext(context.Background(), `
		SELECT task_id, timestamp, text, metadata, status
		FROM tool_task_progress
		ORDER BY task_id, timestamp
	`)
	if err != nil {
		return fmt.Errorf("load tool task progress: %w", err)
	}
	defer progRows.Close()
	for progRows.Next() {
		var (
			taskID  string
			tsValue int64
			text    sql.NullString
			meta    sql.NullString
			status  sql.NullString
		)
		if err := progRows.Scan(&taskID, &tsValue, &text, &meta, &status); err != nil {
			return fmt.Errorf("scan tool task progress: %w", err)
		}
		if task := tasks[strings.TrimSpace(taskID)]; task != nil {
			entry := ProgressEntry{
				Timestamp: time.Unix(0, tsValue).UTC(),
				Text:      strings.TrimSpace(text.String),
				Metadata:  strings.TrimSpace(meta.String),
				Status:    strings.TrimSpace(status.String),
			}
			task.Progress = append(task.Progress, entry)
		}
	}
	if err := progRows.Err(); err != nil {
		return fmt.Errorf("iterate tool task progress: %w", err)
	}
	m.tasks = tasks
	return nil
}

func (m *Manager) resumeIncomplete() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, task := range m.tasks {
		if task == nil {
			continue
		}
		switch task.Status {
		case StatusLoading, StatusPending:
			select {
			case m.queue <- id:
			default:
				go func(taskID string) { m.queue <- taskID }(id)
			}
		}
	}
}
