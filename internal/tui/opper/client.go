package opper

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.opper.ai/v2"

// Option configures an Opper client.
type Option func(*Opper)

type Opper struct {
	// APIKey is used for Authorization: Bearer <APIKey>
	APIKey string
	// BaseURL defaults to https://api.opper.ai/v2
	BaseURL    string
	HTTPClient *http.Client
}

// WithHTTPClient allows supplying a custom HTTP client when constructing Opper via New.
func WithHTTPClient(client *http.Client) Option {
	return func(o *Opper) {
		o.HTTPClient = client
	}
}

// WithBaseURL overrides the default API base URL for clients constructed via New.
func WithBaseURL(baseURL string) Option {
	return func(o *Opper) {
		o.BaseURL = normalizeBaseURL(baseURL)
	}
}

// WithTimeout applies an HTTP client timeout when constructing via New.
func WithTimeout(d time.Duration) Option {
	return func(o *Opper) {
		if o.HTTPClient == nil {
			o.HTTPClient = &http.Client{}
		}
		o.HTTPClient.Timeout = d
	}
}

func New(apiKey string, opts ...Option) *Opper {
	client := &Opper{
		APIKey:     apiKey,
		BaseURL:    defaultBaseURL,
		HTTPClient: &http.Client{Timeout: 0}, // no timeout for streams
	}

	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}

	client.BaseURL = normalizeBaseURL(client.BaseURL)
	if client.HTTPClient == nil {
		client.HTTPClient = &http.Client{Timeout: 0}
	}

	return client
}

// WithBaseURL overrides the default base URL (useful for testing).
func (c *Opper) WithBaseURL(baseURL string) *Opper {
	c.BaseURL = normalizeBaseURL(baseURL)
	return c
}

type Example struct {
	Input   any     `json:"input"`
	Output  any     `json:"output"`
	Comment *string `json:"comment,omitempty"`
}

// StreamRequest is the payload for POST /call/stream.
type StreamRequest struct {
	Name          string            `json:"name"`
	Instructions  *string           `json:"instructions,omitempty"`
	InputSchema   any               `json:"input_schema,omitempty"`
	OutputSchema  any               `json:"output_schema,omitempty"` // ignored by server when streaming
	Input         any               `json:"input,omitempty"`
	Model         any               `json:"model,omitempty"`
	Examples      []Example         `json:"examples,omitempty"`
	ParentSpanID  *string           `json:"parent_span_id,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	Configuration map[string]any    `json:"configuration,omitempty"`
}

// StreamingChunk is the data payload from the server in each SSE event.
// It supports different chunk types with varying fields.
type StreamingChunk struct {
	Delta     any    `json:"delta"` // Can be string, number, bool, or other JSON-compatible type
	SpanID    string `json:"span_id,omitempty"`
	JSONPath  string `json:"json_path,omitempty"`
	ChunkType string `json:"chunk_type,omitempty"`
}

// SSEEvent models a single Server-Sent Event returned by the API.
type SSEEvent struct {
	ID    *string        `json:"id,omitempty"`
	Event *string        `json:"event,omitempty"`
	Data  StreamingChunk `json:"data"`
	Retry *int           `json:"retry,omitempty"`
}

// Stream calls POST /call/stream and returns a channel of SSEEvent.
// Caller should range over the returned channel until it closes.
func (c *Opper) Stream(ctx context.Context, reqBody StreamRequest) (<-chan SSEEvent, error) {
	resp, err := c.doStream(ctx, reqBody)
	if err != nil {
		return nil, err
	}

	out := make(chan SSEEvent)

	go func() {
		defer close(out)
		defer resp.Body.Close()

		_ = streamSSE(resp.Body, func(evt SSEEvent) bool {
			select {
			case out <- evt:
				return true
			case <-ctx.Done():
				return false
			}
		})
	}()

	return out, nil
}

// StreamRaw calls POST /call/stream and returns the raw response body reader.
// This allows direct access to the SSE stream without parsing.
// The caller is responsible for reading and closing the response body.
func (c *Opper) StreamRaw(ctx context.Context, reqBody StreamRequest) (io.ReadCloser, error) {
	resp, err := c.doStream(ctx, reqBody)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

func (c *Opper) doStream(ctx context.Context, reqBody StreamRequest) (*http.Response, error) {
	c.ensureDefaults()

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/call/stream", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parseAPIError(resp)
	}

	return resp, nil
}

func parseAPIError(resp *http.Response) error {
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	// Try common error structure { type, message, detail }
	var apiErr struct {
		Type    string      `json:"type"`
		Message string      `json:"message"`
		Detail  interface{} `json:"detail"`
	}
	if err := json.Unmarshal(body, &apiErr); err == nil && (apiErr.Message != "" || apiErr.Type != "") {
		if apiErr.Detail != nil {
			return fmt.Errorf("api error %s: %s (%v)", apiErr.Type, apiErr.Message, apiErr.Detail)
		}
		return fmt.Errorf("api error %s: %s", apiErr.Type, apiErr.Message)
	}

	if len(body) == 0 {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	return fmt.Errorf("unexpected status %s: %s", resp.Status, string(body))
}

func streamSSE(r io.Reader, emit func(SSEEvent) bool) error {
	reader := bufio.NewReader(r)

	var (
		idStr     *string
		eventStr  *string
		retryInt  *int
		dataBuf   strings.Builder
		keepGoing = true
	)

	reset := func() {
		idStr, eventStr, retryInt = nil, nil, nil
		dataBuf.Reset()
	}

	flush := func() {
		if !keepGoing {
			reset()
			return
		}

		if dataBuf.Len() == 0 && idStr == nil && eventStr == nil && retryInt == nil {
			reset()
			return
		}

		chunk := StreamingChunk{}
		if dataBuf.Len() > 0 {
			if err := json.Unmarshal([]byte(dataBuf.String()), &chunk); err != nil {
				reset()
				return
			}
		}

		if !emit(SSEEvent{ID: idStr, Event: eventStr, Data: chunk, Retry: retryInt}) {
			keepGoing = false
		}
		reset()
	}

	for keepGoing {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")

		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, ":"):
			// comment, ignore
		default:
			field, value := line, ""
			if idx := strings.IndexByte(line, ':'); idx >= 0 {
				field = line[:idx]
				value = line[idx+1:]
				if strings.HasPrefix(value, " ") {
					value = strings.TrimPrefix(value, " ")
				}
			}

			switch field {
			case "id":
				v := value
				idStr = &v
			case "event":
				v := value
				eventStr = &v
			case "retry":
				if v, convErr := strconv.Atoi(value); convErr == nil {
					retryInt = &v
				}
			case "data":
				if dataBuf.Len() > 0 {
					dataBuf.WriteByte('\n')
				}
				dataBuf.WriteString(value)
			}
		}

		if err != nil {
			flush()
			if err == io.EOF {
				return nil
			}
			return err
		}
	}

	return nil
}

func (c *Opper) ensureDefaults() {
	c.BaseURL = normalizeBaseURL(c.BaseURL)
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 0}
	}
}

func normalizeBaseURL(baseURL string) string {
	if baseURL == "" {
		return defaultBaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

// WithTimeout allows setting a dial/read timeout for non-stream endpoints.
// Streaming should usually not set a short timeout; this helper is provided for completeness.
func (c *Opper) WithTimeout(d time.Duration) *Opper {
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{}
	}
	c.HTTPClient.Timeout = d
	return c
}
