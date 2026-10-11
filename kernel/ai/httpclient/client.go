// Package httpclient is the HTTP ai.ModelClient. It speaks the Messages-style protocol to a
// base URL: the fake LLM server in mock mode, a provider endpoint in live mode
// (spec/ai/mocks.md sec. 8). FromConfig is the only place a model client is built at boot.
package httpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
)

var _ ai.ModelClient = (*Client)(nil)

// ErrNoKey is returned by FromConfig when live mode resolves an empty key.
var ErrNoKey = errors.New("httpclient: live model key is empty")

// ErrStatus is returned for a provider status with no more specific sentinel.
var ErrStatus = errors.New("httpclient: unexpected provider status")

// KeyFunc resolves a vault key reference to the key. The key is never logged or wrapped
// into an error.
type KeyFunc func(ctx context.Context, ref string) (string, error)

// Client is an HTTP ModelClient.
type Client struct {
	base   string
	mode   ai.Mode
	key    string
	models map[string]string
	hc     *http.Client
}

// Options configures New.
type Options struct {
	BaseURL string
	Mode    ai.Mode
	Key     string            // live only
	Models  map[string]string // logical name -> provider id
	HTTP    *http.Client      // nil = http.Client with Timeout
	Timeout time.Duration     // per request; 0 = 120s
}

// New returns a Client for opts.
func New(opts Options) *Client {
	hc := opts.HTTP
	if hc == nil {
		t := opts.Timeout
		if t == 0 {
			t = 120 * time.Second
		}
		hc = &http.Client{Timeout: t}
	}
	mode := opts.Mode
	if mode == "" {
		mode = ai.ModeMock
	}
	return &Client{base: strings.TrimRight(opts.BaseURL, "/"), mode: mode, key: opts.Key, models: opts.Models, hc: hc}
}

// FromConfig builds the ModelClient for cfg in environment env. Mock with an empty base_url
// is the in-process aifake over fixtures; mock with a base_url is the fake LLM server; live
// needs base_url and key_ref and is refused in test and CI environments.
func FromConfig(ctx context.Context, cfg ai.Config, env string, fixtures fs.FS, key KeyFunc) (ai.ModelClient, error) {
	mode, err := cfg.Check(env)
	if err != nil {
		return nil, err
	}
	if mode == ai.ModeMock {
		// rule: AI-R-001
		if strings.TrimSpace(cfg.BaseURL) == "" {
			return aifake.New(fixtures), nil
		}
		return New(Options{BaseURL: cfg.BaseURL, Mode: ai.ModeMock, Models: cfg.Models}), nil
	}
	if key == nil {
		return nil, fmt.Errorf("model config: no key resolver: %w", ErrNoKey)
	}
	k, err := key(ctx, cfg.KeyRef)
	if err != nil {
		return nil, fmt.Errorf("model config: resolve key ref: %w", ai.ErrLiveModelForbidden)
	}
	if strings.TrimSpace(k) == "" {
		return nil, fmt.Errorf("model config: %w: %w", ErrNoKey, ai.ErrLiveModelForbidden)
	}
	return New(Options{BaseURL: cfg.BaseURL, Mode: ai.ModeLive, Key: k, Models: cfg.Models}), nil
}

// Mode reports mock or live.
func (c *Client) Mode() ai.Mode { return c.mode }

type wireBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type wireMessage struct {
	Role    string      `json:"role"`
	Content []wireBlock `json:"content"`
}

type wireTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type wireRequest struct {
	Model     string            `json:"model"`
	System    string            `json:"system,omitempty"`
	Messages  []wireMessage     `json:"messages"`
	Tools     []wireTool        `json:"tools,omitempty"`
	MaxTokens int               `json:"max_tokens"`
	Temp      json.RawMessage   `json:"temperature,omitempty"` // passed through from Request.Temperature
	Stream    bool              `json:"stream,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type wireUsage struct {
	InputTokens          int `json:"input_tokens"`
	OutputTokens         int `json:"output_tokens"`
	CacheReadInputTokens int `json:"cache_read_input_tokens"`
}

type wireResponse struct {
	Content    []wireBlock `json:"content"`
	StopReason string      `json:"stop_reason"`
	Usage      wireUsage   `json:"usage"`
}

func (c *Client) wire(req ai.Request, stream bool) wireRequest {
	model := req.Model
	if id, ok := c.models[model]; ok {
		model = id
	}
	max := req.MaxTokens
	if max == 0 {
		max = 1024
	}
	w := wireRequest{Model: model, System: req.System, MaxTokens: max, Stream: stream}
	if req.Temperature != nil {
		w.Temp, _ = json.Marshal(*req.Temperature)
	}
	// Only the feature id and run id travel as metadata; never client content.
	w.Metadata = map[string]string{"feature": req.FeatureID}
	if id := req.Metadata["run_id"]; id != "" {
		w.Metadata["run_id"] = id
	}
	for _, m := range req.Messages {
		role := m.Role
		if role == "tool" {
			role = "user"
		}
		wm := wireMessage{Role: role}
		for _, b := range m.Content {
			wb := wireBlock{Type: b.Type, Text: b.Text, ID: b.ToolUseID, Name: b.ToolName, Input: b.Input, IsError: b.IsError}
			if b.Type == "tool_result" {
				wb = wireBlock{Type: b.Type, ToolUseID: b.ToolUseID, Content: b.Text, IsError: b.IsError}
			}
			wm.Content = append(wm.Content, wb)
		}
		w.Messages = append(w.Messages, wm)
	}
	for _, t := range req.Tools {
		w.Tools = append(w.Tools, wireTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return w
}

func (c *Client) post(ctx context.Context, req ai.Request, stream bool) (*http.Response, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(c.wire(req, stream))
	if err != nil {
		return nil, fmt.Errorf("model request: encode: %w", err)
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("model request: build: %w", err)
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("X-Counsel-Feature", req.FeatureID)
	if id := req.Metadata["run_id"]; id != "" {
		hr.Header.Set("X-Counsel-Run", id)
	}
	if c.key != "" {
		hr.Header.Set("x-api-key", c.key)
	}
	resp, err := c.hc.Do(hr)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			return nil, fmt.Errorf("model request: %w", ai.ErrTimeout)
		}
		return nil, fmt.Errorf("model request: transport failed: %w", ctxOr(ctx, err))
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var e struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
		return nil, statusError(resp.StatusCode, e.Error.Type, resp.Header.Get("Retry-After"))
	}
	return resp, nil
}

// ctxOr prefers the context error, so a cancelled call reads as cancelled. Transport errors
// carry the URL only; the key travels in a header and never reaches an error.
func ctxOr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

func statusError(code int, typ, retryAfter string) error {
	switch {
	case code == http.StatusTooManyRequests:
		return fmt.Errorf("model request: status 429 retry-after %q: %w", retryAfter, ai.ErrRateLimited)
	case code == 529 || code == http.StatusServiceUnavailable:
		return fmt.Errorf("model request: status %d: %w", code, ai.ErrOverloaded)
	case code == http.StatusGatewayTimeout || code == http.StatusRequestTimeout:
		return fmt.Errorf("model request: status %d: %w", code, ai.ErrTimeout)
	case typ == "no_fixture":
		return fmt.Errorf("model request: status %d: %w", code, ai.ErrNoFixture)
	}
	return fmt.Errorf("model request: status %d type %q: %w", code, typ, ErrStatus)
}

func toResponse(w wireResponse) ai.Response {
	r := ai.Response{StopReason: w.StopReason, Usage: ai.Usage{InputTokens: w.Usage.InputTokens,
		OutputTokens: w.Usage.OutputTokens, CacheReadTokens: w.Usage.CacheReadInputTokens}}
	for _, b := range w.Content {
		switch b.Type {
		case "text":
			r.Text += b.Text
		case "tool_use":
			r.ToolCalls = append(r.ToolCalls, ai.ToolCall{ID: b.ID, Name: b.Name, Input: b.Input})
		}
	}
	return r
}

// Complete runs one non-streaming request.
func (c *Client) Complete(ctx context.Context, req ai.Request) (ai.Response, error) {
	resp, err := c.post(ctx, req, false)
	if err != nil {
		return ai.Response{}, err
	}
	defer resp.Body.Close()
	var w wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&w); err != nil {
		return ai.Response{}, fmt.Errorf("model response: decode: %w", ai.ErrBadOutput)
	}
	return toResponse(w), nil
}

type sseEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		Usage wireUsage `json:"usage"`
	} `json:"message"`
	ContentBlock wireBlock `json:"content_block"`
	Delta        struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage wireUsage `json:"usage"`
}

// Stream runs one streaming request. The channel closes after a Done or Err event.
func (c *Client) Stream(ctx context.Context, req ai.Request) (<-chan ai.StreamEvent, error) {
	resp, err := c.post(ctx, req, true)
	if err != nil {
		return nil, err
	}
	ch := make(chan ai.StreamEvent)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		send := func(ev ai.StreamEvent) bool {
			select {
			case ch <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		var (
			final ai.Response
			calls = map[int]*ai.ToolCall{}
			order []int
		)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64<<10), 16<<20)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			var ev sseEvent
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &ev); err != nil {
				send(ai.StreamEvent{Err: fmt.Errorf("model stream: decode: %w", ai.ErrBadOutput)})
				return
			}
			switch ev.Type {
			case "message_start":
				final.Usage.InputTokens = ev.Message.Usage.InputTokens
				final.Usage.CacheReadTokens = ev.Message.Usage.CacheReadInputTokens
			case "content_block_start":
				if ev.ContentBlock.Type == "tool_use" {
					calls[ev.Index] = &ai.ToolCall{ID: ev.ContentBlock.ID, Name: ev.ContentBlock.Name}
					order = append(order, ev.Index)
				}
			case "content_block_delta":
				switch ev.Delta.Type {
				case "text_delta":
					final.Text += ev.Delta.Text
					if !send(ai.StreamEvent{TextDelta: ev.Delta.Text}) {
						return
					}
				case "input_json_delta":
					tc := calls[ev.Index]
					if tc == nil {
						continue
					}
					tc.Input = append(tc.Input, ev.Delta.PartialJSON...)
					if !send(ai.StreamEvent{ToolCallDelta: &ai.ToolCall{ID: tc.ID, Name: tc.Name, Input: json.RawMessage(ev.Delta.PartialJSON)}}) {
						return
					}
				}
			case "message_delta":
				final.StopReason = ev.Delta.StopReason
				final.Usage.OutputTokens = ev.Usage.OutputTokens
			case "message_stop":
				for _, i := range order {
					tc := *calls[i]
					if len(tc.Input) == 0 {
						tc.Input = json.RawMessage("{}")
					}
					final.ToolCalls = append(final.ToolCalls, tc)
				}
				send(ai.StreamEvent{Done: &final})
				return
			case "error":
				send(ai.StreamEvent{Err: fmt.Errorf("model stream: provider error: %w", ErrStatus)})
				return
			}
		}
		err := sc.Err()
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		send(ai.StreamEvent{Err: fmt.Errorf("model stream: ended before message_stop: %w", ctxOr(ctx, err))})
	}()
	return ch, nil
}
