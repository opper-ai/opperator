package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"tui/opper"
	tooltypes "tui/tools/types"
)

// networkLogger captures request/response details for debugging network calls.
type networkLogger interface {
	LogRequest(kind string, req opper.StreamRequest)
	LogOutput(kind string, content string, toolCalls []tooltypes.Call, toolResults []tooltypes.Result)
	LogDelta(kind string, delta string)
	LogToolDelta(kind, id, name, delta string)
}

// fileNetworkLogger appends structured entries to a local log file.
type fileNetworkLogger struct {
	path string
	seq  atomic.Uint64
}

func newFileNetworkLogger(path string) *fileNetworkLogger {
	return &fileNetworkLogger{path: path}
}

func (l *fileNetworkLogger) nextID() uint64 {
	return l.seq.Add(1)
}

func (l *fileNetworkLogger) LogRequest(kind string, req opper.StreamRequest) {
	if l == nil {
		return
	}
	b, _ := json.MarshalIndent(req, "", "  ")
	label := "REQUEST"
	if kind != "" {
		label = fmt.Sprintf("%s %s", label, kind)
	}
	body := fmt.Sprintf("Model: %v\nPayload:\n%s", req.Model, string(b))
	l.append(label, body)
}

func (l *fileNetworkLogger) LogOutput(kind string, content string, toolCalls []tooltypes.Call, toolResults []tooltypes.Result) {
	if l == nil {
		return
	}
	entry := map[string]any{"content": content}
	if len(toolCalls) > 0 {
		entry["tool_calls"] = toolCalls
	}
	if len(toolResults) > 0 {
		entry["tool_results"] = toolResults
	}
	b, _ := json.MarshalIndent(entry, "", "  ")
	label := "OUTPUT"
	if kind != "" {
		label = fmt.Sprintf("%s %s", label, kind)
	}
	body := fmt.Sprintf("Payload:\n%s", string(b))
	l.append(label, body)
}

func (l *fileNetworkLogger) LogDelta(kind string, delta string) {
	if l == nil || delta == "" {
		return
	}
	entry := map[string]any{"delta": delta}
	b, _ := json.MarshalIndent(entry, "", "  ")
	label := "DELTA"
	if kind != "" {
		label = fmt.Sprintf("%s %s", label, kind)
	}
	body := fmt.Sprintf("Payload:\n%s", string(b))
	l.append(label, body)
}

func (l *fileNetworkLogger) LogToolDelta(kind, id, name, delta string) {
	if l == nil || (name == "" && delta == "") {
		return
	}
	entry := map[string]any{
		"tool_id": id,
		"name":    name,
		"delta":   delta,
	}
	b, _ := json.MarshalIndent(entry, "", "  ")
	label := "DELTA"
	if kind != "" {
		label = fmt.Sprintf("%s %s", label, kind)
	}
	body := fmt.Sprintf("Payload:\n%s", string(b))
	l.append(label, body)
}

func (l *fileNetworkLogger) append(label, body string) {
	if l == nil {
		return
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	id := l.nextID()
	ts := time.Now().Format(time.RFC3339)
	fmt.Fprintf(f, "\n===== %s #%d =====\nTime: %s\n%s\n", label, id, ts, body)
}
