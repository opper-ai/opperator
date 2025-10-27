package llm

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"

	"tui/internal/keyring"
	"tui/internal/opper"
	"tui/lsp"
	"tui/permission"
	"tui/secretprompt"
	tooling "tui/tools"
	tooltypes "tui/tools/types"
)

// Engine coordinates streaming completions and tool execution for the TUI.
type Engine struct {
	runner        toolRunner
	permissions   permission.Service
	workingDir    string
	invocationDir string
	async         *asyncTracker
	secretPrompts secretprompt.Service
}

func NewEngine(permissions permission.Service, secrets secretprompt.Service, workingDir string, invocationDir string, manager *lsp.Manager) *Engine {
	trimmedInvocation := strings.TrimSpace(invocationDir)
	if trimmedInvocation == "" {
		trimmedInvocation = workingDir
	}
	return &Engine{
		runner:        newToolRunner(permissions, secrets, workingDir, trimmedInvocation, manager),
		permissions:   permissions,
		workingDir:    workingDir,
		invocationDir: trimmedInvocation,
		async:         newAsyncTracker(),
		secretPrompts: secrets,
	}
}

func (e *Engine) AsyncUpdates() <-chan AsyncToolUpdateMsg {
	if e == nil || e.async == nil {
		return nil
	}
	return e.async.Updates()
}

func (e *Engine) RestoreAsyncTasks(tasks []tooling.AsyncTask) {
	if e == nil || e.async == nil {
		return
	}
	e.async.Restore(tasks)
}

func (e *Engine) trackAsyncTask(reg asyncTaskRegistration) {
	if e == nil || e.async == nil {
		return
	}
	e.async.Watch(reg)
}

type streamPhaseResult struct {
	content     string
	toolCalls   []tooltypes.Call
	toolResults []tooltypes.Result
	hadActivity bool
	spanID      string
}

type streamToolCollector struct {
	label    string
	ch       chan tea.Msg
	builders map[int]*streamToolBuilder
}

type streamToolBuilder struct {
	idx     int
	id      string
	name    strings.Builder
	args    strings.Builder
	started bool
}

func newToolStreamCollector(label string, ch chan tea.Msg) *streamToolCollector {
	return &streamToolCollector{
		label:    label,
		ch:       ch,
		builders: make(map[int]*streamToolBuilder),
	}
}

func (c *streamToolCollector) ensure(idx int) *streamToolBuilder {
	if idx < 0 {
		idx = 0
	}
	if b, ok := c.builders[idx]; ok {
		return b
	}
	b := &streamToolBuilder{idx: idx, id: generateToolCallID()}
	c.builders[idx] = b
	return b
}

func (c *streamToolCollector) idForIndex(idx int) string {
	return c.ensure(idx).id
}

func (b *streamToolBuilder) currentName() string {
	return strings.TrimSpace(b.name.String())
}

func (b *streamToolBuilder) setName(name string) {
	b.name.Reset()
	b.name.WriteString(name)
}

func (c *streamToolCollector) handle(path, delta string) {
	if delta == "" {
		return
	}
	tokens, err := parseJSONPath(path)
	if err != nil || len(tokens) < 3 {
		return
	}
	if tokens[0].key != "result" || tokens[1].key != "tools" || tokens[1].index < 0 {
		return
	}
	if tokens[2].key != "arguments" {
		return
	}
	builder := c.ensure(tokens[1].index)
	c.emitStart(builder)
	builder.args.WriteString(delta)
	c.ch <- ToolUseDeltaMsg{ID: builder.id, Delta: delta}
}

func (c *streamToolCollector) emitStart(builder *streamToolBuilder) {
	if builder == nil || builder.started {
		return
	}
	builder.started = true
	c.ch <- ToolUseStartMsg{Call: tooltypes.Call{ID: builder.id, Name: builder.currentName(), Input: builder.args.String(), Finished: false}}
}

func (c *streamToolCollector) syncName(idx int, name string) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return
	}
	builder := c.ensure(idx)
	if builder.currentName() == trimmed {
		return
	}
	builder.setName(trimmed)
	if builder.started {
		c.ch <- ToolUseDeltaMsg{ID: builder.id, Name: trimmed}
	}
}

type sessionOutput struct {
	Text  string                 `json:"text"`
	Tools []sessionOutputTool    `json:"tools"`
	Meta  map[string]interface{} `json:"meta,omitempty"`
}

type sessionOutputTool struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
	Reason    string                 `json:"reason"`
}

type sessionToolCall struct {
	ID        string
	Name      string
	Arguments map[string]interface{}
	started   bool
	Reason    string
}

// Request prepares a Bubble Tea command that starts streaming when executed.
// caller should poll via tea.Cmds.
func (e *Engine) Request(adapter Adapter) (tea.Cmd, context.CancelFunc, chan tea.Msg) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan tea.Msg, 64)

	cmd := func() tea.Msg {
		apiKey, err := keyring.GetAPIKey()
		if err != nil {
			close(ch)
			cancel()
			if errors.Is(err, keyring.ErrNotFound) {
				return StreamStartedMsg{Err: fmt.Errorf("Opper API key is not configured. Run `./opperator secret create --name=%s` to store one", keyring.OpperAPIKeyName)}
			}
			return StreamStartedMsg{Err: fmt.Errorf("failed to read Opper API key: %w", err)}
		}

		specs := append([]tooling.Spec{}, adapter.BaseToolSpecs()...)
		specs = append(specs, adapter.ExtraToolSpecs()...)
		if spec := adapter.AgentToolSpec(); strings.TrimSpace(spec.Name) != "" {
			specs = append(specs, spec)
		}

		client := opper.New(apiKey)
		go e.runSession(ctx, cancel, adapter, ch, client, specs)

		return StreamStartedMsg{}
	}

	return cmd, cancel, ch
}

func (e *Engine) runSession(
	ctx context.Context,
	cancel context.CancelFunc,
	adapter Adapter,
	ch chan tea.Msg,
	client *opper.Opper,
	specs []tooling.Spec,
) {
	defer close(ch)
	defer cancel()

	turnStart := time.Now()

	req := e.buildStreamRequest(adapter, specs)

	res, err := e.streamPhase(ctx, adapter, ch, client, req, "INITIAL", "TOOL RESULTS")
	if err != nil {
		adapter.RecordTurnCompletion(time.Since(turnStart))
		ch <- StreamDoneMsg{Err: err}
		return
	}

	if res.spanID != "" {
		adapter.RecordSpanID(res.spanID)
	}

	if len(res.toolCalls) > 0 {
		adapter.RecordAssistantToolCalls(res.toolCalls, res.content)
		adapter.RecordToolResults(res.toolResults)
		e.followUpLoop(ctx, adapter, ch, client, specs, 1, turnStart)
		return
	}

	if !res.hadActivity {
		adapter.RecordTurnCompletion(time.Since(turnStart))
		// Check if the context was cancelled - if so, return that error instead of "empty streaming response"
		if ctx.Err() != nil {
			ch <- StreamDoneMsg{Err: ctx.Err()}
		} else {
			ch <- StreamDoneMsg{Err: fmt.Errorf("empty streaming response")}
		}
		return
	}

	adapter.RecordAssistantContent(res.content)
	adapter.RecordTurnCompletion(time.Since(turnStart))
	ch <- StreamDoneMsg{}
}

func (e *Engine) followUpLoop(
	ctx context.Context,
	adapter Adapter,
	ch chan tea.Msg,
	client *opper.Opper,
	specs []tooling.Spec,
	pass int,
	turnStart time.Time,
) {
	currentPass := pass
	for {
		if currentPass > maxFollowPasses {
			adapter.RecordTurnCompletion(time.Since(turnStart))
			ch <- StreamDoneMsg{Err: fmt.Errorf("max follow-up passes reached")}
			return
		}

		ch <- FollowupStartMsg{}

		label := fmt.Sprintf("FOLLOW-UP %d", currentPass)
		resultsLabel := fmt.Sprintf("TOOL RESULTS %d", currentPass)
		req := e.buildStreamRequest(adapter, specs)

		res, err := e.streamPhase(ctx, adapter, ch, client, req, label, resultsLabel)
		if err != nil {
			adapter.RecordTurnCompletion(time.Since(turnStart))
			ch <- StreamDoneMsg{Err: err}
			return
		}

		if res.spanID != "" {
			adapter.RecordSpanID(res.spanID)
		}

		if len(res.toolCalls) > 0 {
			adapter.RecordAssistantToolCalls(res.toolCalls, res.content)
			adapter.RecordToolResults(res.toolResults)
			currentPass++
			continue
		}

		trimmed := strings.TrimSpace(res.content)
		last := adapter.LastAssistantContent()
		if trimmed == "" || strings.EqualFold(trimmed, last) {
			adapter.RecordTurnCompletion(time.Since(turnStart))
			ch <- StreamDoneMsg{}
			return
		}

		adapter.RecordAssistantContent(trimmed)
		adapter.RecordTurnCompletion(time.Since(turnStart))
		ch <- StreamDoneMsg{}
		return
	}
}

func (e *Engine) streamPhase(
	ctx context.Context,
	adapter Adapter,
	ch chan tea.Msg,
	client *opper.Opper,
	req opper.StreamRequest,
	label string,
	resultsLabel string,
) (streamPhaseResult, error) {
	var res streamPhaseResult

	events, err := client.Stream(ctx, req)
	if err != nil {
		return res, err
	}

	aggregator := newJSONChunkAggregator()
	collector := newToolStreamCollector(label, ch)
	var textBuilder strings.Builder
	var spanID string

	for event := range events {
		chunk := event.Data
		if spanID == "" && chunk.SpanID != "" {
			spanID = chunk.SpanID
		}
		if chunk.JSONPath != "" || chunk.ChunkType == "json" {
			path := chunk.JSONPath
			if path == "" {
				path = "text"
			}
			aggregator.Add(path, chunk.Delta)
			collector.handle(path, chunk.Delta)
			// Only capture deltas for the "text" field (can be "text" or "result.text")
			// This excludes meta fields and other non-display content
			isTextField := path == "text" || strings.HasSuffix(path, ".text")
			if isTextField && chunk.Delta != "" {
				res.hadActivity = true
				textBuilder.WriteString(chunk.Delta)
				ch <- StreamDeltaMsg{Text: chunk.Delta}
			}
			continue
		}
		if chunk.Delta != "" {
			res.hadActivity = true
			textBuilder.WriteString(chunk.Delta)
			ch <- StreamDeltaMsg{Text: chunk.Delta}
		}
	}

	res.spanID = spanID

	assembled, err := aggregator.Assemble()
	if err != nil {
		return res, fmt.Errorf("assemble json: %w", err)
	}

	var output sessionOutput
	if assembled != "" {
		if err := json.Unmarshal([]byte(assembled), &output); err != nil {
			var wrapper struct {
				Result sessionOutput `json:"result"`
			}
			if err := json.Unmarshal([]byte(assembled), &wrapper); err != nil {
				return res, fmt.Errorf("decode streaming output: %w (raw: %s)", err, assembled)
			}
			output = wrapper.Result
		}
	}

	// Use textBuilder as primary source since it captures all streamed text deltas
	// Falls back to output.Text from assembled JSON if textBuilder is empty
	textContent := strings.TrimSpace(textBuilder.String())
	if textContent != "" {
		res.hadActivity = true
		res.content = textContent
	} else if output.Text != "" {
		res.hadActivity = true
		res.content = strings.TrimSpace(output.Text)
	}

	calls := make([]sessionToolCall, 0, len(output.Tools))
	for idx, tool := range output.Tools {
		name := strings.TrimSpace(tool.Name)
		collector.syncName(idx, name)
		builder := collector.ensure(idx)
		if name == "" {
			name = builder.currentName()
		}
		if name == "" {
			continue
		}
		calls = append(calls, sessionToolCall{
			ID:        builder.id,
			Name:      name,
			Arguments: tool.Arguments,
			started:   builder.started,
			Reason:    strings.TrimSpace(tool.Reason),
		})
	}

	if len(calls) > 0 {
		res.hadActivity = true
		cmCalls, results := e.executeToolCalls(ctx, adapter, calls, ch)
		res.toolCalls = cmCalls
		res.toolResults = results
	}

	return res, nil
}

func (e *Engine) executeToolCalls(ctx context.Context, adapter Adapter, calls []sessionToolCall, ch chan tea.Msg) ([]tooltypes.Call, []tooltypes.Result) {
	if len(calls) == 0 {
		return nil, nil
	}

	argsJSONs := make([]string, len(calls))
	cmpCalls := make([]tooltypes.Call, 0, len(calls))
	for idx, call := range calls {
		argsJSON := marshalArgs(call.Arguments)
		argsJSONs[idx] = argsJSON
		cmpCalls = append(cmpCalls, tooltypes.Call{ID: call.ID, Name: call.Name, Input: argsJSON, Finished: true, Reason: call.Reason})
	}

	results := make([]tooltypes.Result, len(calls))
	allowed := make([]bool, len(calls))
	denyMessages := make([]string, len(calls))
	for idx, call := range calls {
		argsJSON := argsJSONs[idx]
		allowed[idx], denyMessages[idx] = e.allowToolExecution(adapter, call, argsJSON)
		if !allowed[idx] {
			if !call.started {
				ch <- ToolUseStartMsg{Call: tooltypes.Call{ID: call.ID, Name: call.Name, Input: argsJSON, Finished: false, Reason: call.Reason}}
			}
			msg := permission.ErrorPermissionDenied.Error()
			if trimmed := strings.TrimSpace(denyMessages[idx]); trimmed != "" {
				msg = trimmed
			}
			result := tooltypes.Result{
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    msg,
			}
			results[idx] = result
			ch <- ToolUseFinishMsg{Result: result}
		}
	}

	for idx, call := range calls {
		if !allowed[idx] {
			continue
		}
		argsJSON := argsJSONs[idx]
		if !call.started {
			ch <- ToolUseStartMsg{Call: tooltypes.Call{ID: call.ID, Name: call.Name, Input: argsJSON, Finished: false, Reason: call.Reason}}
		}

		sessionID := strings.TrimSpace(adapter.SessionID())
		toolCtx := tooling.WithSessionContext(ctx, sessionID, call.ID)
		toolCtx = tooling.WithAgentContext(toolCtx, adapter.ActiveAgentName(), adapter.CoreAgentID())

		content, metadata := e.runner.Execute(toolCtx, call.Name, argsJSON, func(ev SubAgentEvent) {
			if ev.ToolCallID == "" {
				ev.ToolCallID = call.ID
			}
			ch <- SubAgentEventMsg{ID: call.ID, Ev: ev}
		})

		if asyncMeta, ok := parseAsyncMetadata(metadata); ok {
			actualTool := strings.TrimSpace(asyncMeta.Tool)
			if actualTool == "" {
				actualTool = tooling.AsyncToolName
			}
			canonicalName := strings.TrimSpace(cmpCalls[idx].Name)
			if canonicalName == "" {
				canonicalName = strings.TrimSpace(call.Name)
			}
			if canonicalName == "" {
				canonicalName = tooling.AsyncToolName
			}
			preserveCanonical := tooling.IsAgentCommandToolName(canonicalName)
			cmpCalls[idx].Finished = false
			if trimmed := strings.TrimSpace(content); trimmed != "" {
				ch <- ToolUseDeltaMsg{ID: call.ID, Delta: trimmed}
			}
			resultName := actualTool
			registrationName := actualTool
			if preserveCanonical {
				resultName = canonicalName
				registrationName = canonicalName
			} else {
				cmpCalls[idx].Name = actualTool
				call.Name = actualTool
				ch <- ToolUseDeltaMsg{ID: call.ID, Name: actualTool}
			}
			asyncSession := strings.TrimSpace(asyncMeta.SessionID)
			if asyncSession == "" {
				asyncSession = sessionID
			}
			asyncCallID := strings.TrimSpace(asyncMeta.CallID)
			if asyncCallID == "" {
				asyncCallID = call.ID
			}
			placeholder := strings.TrimSpace(content)
			if placeholder == "" {
				placeholder = fmt.Sprintf("async task %s pending", strings.TrimSpace(asyncMeta.TaskID))
			}
			results[idx] = tooltypes.Result{
				ToolCallID: asyncCallID,
				Name:       resultName,
				Content:    placeholder,
				Metadata:   metadata,
				Pending:    true,
			}
			e.trackAsyncTask(asyncTaskRegistration{
				TaskID:    strings.TrimSpace(asyncMeta.TaskID),
				SessionID: asyncSession,
				CallID:    asyncCallID,
				ToolName:  registrationName,
			})
			continue
		}
		result := tooltypes.Result{ToolCallID: call.ID, Name: call.Name, Content: content, Metadata: metadata}
		results[idx] = result
		ch <- ToolUseFinishMsg{Result: result}
	}

	orderedResults := make([]tooltypes.Result, 0, len(results))
	for _, call := range calls {
		for _, result := range results {
			if result.ToolCallID == call.ID {
				orderedResults = append(orderedResults, result)
				break
			}
		}
	}

	return cmpCalls, orderedResults
}

func (e *Engine) allowToolExecution(adapter Adapter, call sessionToolCall, argsJSON string) (bool, string) {
	if e.permissions == nil {
		return true, ""
	}

	sessionID := ""
	if adapter != nil {
		sessionID = adapter.SessionID()
	}

	return requestToolPermission(e.permissions, e.workingDir, sessionID, call.ID, call.Name, argsJSON, call.Reason)
}

func (e *Engine) resolvePath(path string) (string, error) {
	return tooling.ResolveWorkingPath(e.workingDir, path)
}

func truncateForPermission(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= 60 {
		return trimmed
	}
	return string(runes[:60]) + "…"
}

func (e *Engine) buildStreamRequest(adapter Adapter, specs []tooling.Spec) opper.StreamRequest {
	instructions := strings.TrimSpace(adapter.BuildInstructions())
	conv := adapter.BuildConversation()

	input := map[string]any{
		"conversation": conv,
	}

	if defs := tooling.SpecsToAPIDefinitions(specs); len(defs) > 0 {
		input["tools"] = defs
	}

	req := opper.StreamRequest{
		Name:         "opperator.session",
		Input:        input,
		OutputSchema: sessionOutputSchema(),
		Model:        modelIdentifier(),
	}
	if instructions != "" {
		req.Instructions = &instructions
	}
	if parent := strings.TrimSpace(adapter.ParentSpanID()); parent != "" {
		req.ParentSpanID = &parent
	}
	return req
}

func sessionOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": "Assistant response text",
			},
			"tools": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":      map[string]any{"type": "string"},
						"arguments": map[string]any{"type": "object"},
						"reason": map[string]any{
							"type":        "string",
							"description": "Very short tool explaining why the tool was chosen or the reasoning behind using it",
						},
					},
					"required": []string{"name"},
				},
			},
		},
		"required": []string{"text"},
	}
}

type asyncMetadata struct {
	TaskID    string `json:"id"`
	Status    string `json:"status"`
	Tool      string `json:"tool"`
	SessionID string `json:"session_id"`
	CallID    string `json:"call_id"`
}

func parseAsyncMetadata(raw string) (asyncMetadata, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return asyncMetadata{}, false
	}
	var wrapper struct {
		Async asyncMetadata `json:"async_task"`
		Task  asyncMetadata `json:"task"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err != nil {
		return asyncMetadata{}, false
	}
	meta := wrapper.Async
	if strings.TrimSpace(meta.TaskID) == "" {
		meta = wrapper.Task
	}
	if strings.TrimSpace(meta.TaskID) == "" {
		return asyncMetadata{}, false
	}
	return meta, true
}

func modelIdentifier() any {
	name := strings.TrimSpace(ModelName())
	if name == "" {
		return "openai/gpt-5-mini"
	}
	if strings.Contains(name, "/") {
		return name
	}
	return "openai/" + name
}

func marshalArgs(args map[string]interface{}) string {
	if len(args) == 0 {
		return "{}"
	}
	b, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func generateToolCallID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("call_%d", time.Now().UnixNano())
	}
	return "call_" + base64.RawURLEncoding.EncodeToString(b)
}
