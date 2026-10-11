package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dagi377/counsel/kernel/ai"
)

// Messages-style protocol: POST /v1/messages and /v1/messages/count_tokens.

const maxBody = 16 << 20

type messagesRequest struct {
	Model    string            `json:"model"`
	System   json.RawMessage   `json:"system"` // a string or a list of text blocks
	Messages []messagesMessage `json:"messages"`
	Tools    []messagesTool    `json:"tools"`
	Stream   bool              `json:"stream"`
	Metadata map[string]any    `json:"metadata"`
}

type messagesMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // a string or a list of blocks
}

type messagesBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"` // tool_result: a string or text blocks
	IsError   bool            `json:"is_error,omitempty"`
}

type messagesTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type messagesUsage struct {
	InputTokens          int `json:"input_tokens"`
	OutputTokens         int `json:"output_tokens"`
	CacheReadInputTokens int `json:"cache_read_input_tokens"`
}

type messagesResponse struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	Role         string          `json:"role"`
	Model        string          `json:"model"`
	Content      []messagesBlock `json:"content"`
	StopReason   *string         `json:"stop_reason"`
	StopSequence *string         `json:"stop_sequence"`
	Usage        messagesUsage   `json:"usage"`
}

// blocks decodes a content field: a bare string is one text block.
func blocks(raw json.RawMessage) ([]messagesBlock, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("content string: %w", err)
		}
		return []messagesBlock{{Type: "text", Text: s}}, nil
	}
	var bs []messagesBlock
	if err := json.Unmarshal(raw, &bs); err != nil {
		return nil, fmt.Errorf("content blocks: %w", err)
	}
	return bs, nil
}

// joinText concatenates the text blocks of a content field.
func joinText(raw json.RawMessage) (string, error) {
	bs, err := blocks(raw)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, b := range bs {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

func metaString(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// toCall maps a Messages request onto kernel/ai types, so the prompt hash is the one
// fixture.PromptHash computes for the same ai.Request.
func (q messagesRequest) toCall(r *http.Request) (call, error) {
	system, err := joinText(q.System)
	if err != nil {
		return call{}, fmt.Errorf("system: %w", err)
	}
	c := call{system: system}
	for i, m := range q.Messages {
		bs, err := blocks(m.Content)
		if err != nil {
			return call{}, fmt.Errorf("messages[%d]: %w", i, err)
		}
		msg := ai.Message{Role: m.Role}
		for _, b := range bs {
			cb := ai.ContentBlock{Type: b.Type, Text: b.Text, IsError: b.IsError}
			switch b.Type {
			case "tool_use":
				cb.ToolUseID, cb.ToolName, cb.Input = b.ID, b.Name, b.Input
			case "tool_result":
				cb.ToolUseID = b.ToolUseID
				if cb.Text, err = joinText(b.Content); err != nil {
					return call{}, fmt.Errorf("messages[%d] tool_result: %w", i, err)
				}
			}
			msg.Content = append(msg.Content, cb)
		}
		c.messages = append(c.messages, msg)
	}
	for _, t := range q.Tools {
		c.tools = append(c.tools, ai.ToolSpec{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	// Feature: header, else metadata.feature, else a "feature: F-..." first system line.
	c.feature = r.Header.Get("X-Counsel-Feature")
	if c.feature == "" {
		c.feature = metaString(q.Metadata, "feature")
	}
	if c.feature == "" {
		c.feature = featureFromSystem(system)
	}
	c.runID = r.Header.Get("X-Counsel-Run")
	if c.runID == "" {
		c.runID = metaString(q.Metadata, "run_id")
	}
	if c.runID == "" {
		c.runID = runFromSystem(system)
	}
	return c, nil
}

func decodeMessages(w http.ResponseWriter, r *http.Request) (messagesRequest, call, *apiError) {
	var q messagesRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&q); err != nil {
		return q, call{}, &apiError{status: http.StatusBadRequest, typ: "invalid_request_error", message: "decode body: " + err.Error()}
	}
	c, err := q.toCall(r)
	if err != nil {
		return q, call{}, &apiError{status: http.StatusBadRequest, typ: "invalid_request_error", message: err.Error()}
	}
	return q, c, nil
}

func writeMessagesError(w http.ResponseWriter, e *apiError) {
	body := map[string]any{"type": e.typ, "message": e.message}
	for k, v := range e.extra {
		body[k] = v
	}
	writeJSON(w, e.status, map[string]any{"type": "error", "error": body})
}

// messages answers POST /v1/messages from a fixture; nothing here can reach a real model.
// rule: AI-R-001
func (s *server) messages(w http.ResponseWriter, r *http.Request) {
	q, c, e := decodeMessages(w, r)
	if e != nil {
		writeMessagesError(w, e)
		return
	}
	fx, t, n, e := s.route(c)
	if e != nil {
		slog.Info("llm fake: call refused", "protocol", "messages", "feature", c.feature, "status", e.status, "type", e.typ)
		writeMessagesError(w, e)
		return
	}
	rep := buildReply(fx, t, n, c)
	slog.Info("llm fake: call answered", "protocol", "messages", "feature", c.feature, "fixture", rep.fixture, "turn", n, "stream", q.Stream)
	if q.Stream {
		if err := rep.stream(w, q.Model); err != nil {
			slog.Warn("llm fake: stream", "feature", c.feature, "err", err)
		}
		return
	}
	stop := rep.stopReason
	writeJSON(w, http.StatusOK, messagesResponse{ID: "msg_" + rep.id, Type: "message", Role: "assistant", Model: q.Model,
		Content: rep.content(), StopReason: &stop, Usage: rep.usageBody()})
}

func (r reply) content() []messagesBlock {
	var out []messagesBlock
	if r.text != "" {
		out = append(out, messagesBlock{Type: "text", Text: r.text})
	}
	for _, tc := range r.toolCalls {
		out = append(out, messagesBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: tc.Input})
	}
	if out == nil {
		out = []messagesBlock{}
	}
	return out
}

func (r reply) usageBody() messagesUsage {
	return messagesUsage{InputTokens: r.usage.InputTokens, OutputTokens: r.usage.OutputTokens, CacheReadInputTokens: r.usage.CacheReadTokens}
}

// countTokens answers POST /v1/messages/count_tokens with the deterministic estimate.
func (s *server) countTokens(w http.ResponseWriter, r *http.Request) {
	_, c, e := decodeMessages(w, r)
	if e != nil {
		writeMessagesError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"input_tokens": estimateTokens(promptChars(c))})
}

// sse writes one server-sent event and flushes it.
type sse struct {
	w  http.ResponseWriter
	fl http.Flusher
}

func (s sse) event(name string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s: %w", name, err)
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, b); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if s.fl != nil {
		s.fl.Flush()
	}
	return nil
}

// chunkJSON splits a JSON document into fragments of at most size bytes on rune boundaries,
// as input_json_delta partial_json pieces.
func chunkJSON(s string, size int) []string {
	var out []string
	var cur strings.Builder
	for _, r := range s {
		if cur.Len() > 0 && cur.Len()+len(string(r)) > size {
			out = append(out, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

const jsonChunk = 16

// stream renders rep as the Messages SSE sequence: message_start, then per content block
// content_block_start / content_block_delta (text_delta or input_json_delta) /
// content_block_stop, then message_delta (stop reason, usage) and message_stop.
func (rep reply) stream(w http.ResponseWriter, model string) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	out := sse{w: w, fl: fl}

	start := messagesResponse{ID: "msg_" + rep.id, Type: "message", Role: "assistant", Model: model,
		Content: []messagesBlock{}, Usage: messagesUsage{InputTokens: rep.usage.InputTokens, OutputTokens: 1, CacheReadInputTokens: rep.usage.CacheReadTokens}}
	if err := out.event("message_start", map[string]any{"type": "message_start", "message": start}); err != nil {
		return err
	}
	idx := 0
	block := func(startBlock map[string]any, deltas []map[string]any) error {
		if err := out.event("content_block_start", map[string]any{"type": "content_block_start", "index": idx, "content_block": startBlock}); err != nil {
			return err
		}
		for _, d := range deltas {
			if err := out.event("content_block_delta", map[string]any{"type": "content_block_delta", "index": idx, "delta": d}); err != nil {
				return err
			}
		}
		if err := out.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx}); err != nil {
			return err
		}
		idx++
		return nil
	}
	if rep.text != "" {
		var deltas []map[string]any
		for _, word := range strings.SplitAfter(rep.text, " ") {
			if word != "" {
				deltas = append(deltas, map[string]any{"type": "text_delta", "text": word})
			}
		}
		if err := block(map[string]any{"type": "text", "text": ""}, deltas); err != nil {
			return err
		}
	}
	for _, tc := range rep.toolCalls {
		var deltas []map[string]any
		for _, part := range chunkJSON(string(tc.Input), jsonChunk) {
			deltas = append(deltas, map[string]any{"type": "input_json_delta", "partial_json": part})
		}
		if err := block(map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": map[string]any{}}, deltas); err != nil {
			return err
		}
	}
	if err := out.event("message_delta", map[string]any{"type": "message_delta",
		"delta": map[string]any{"stop_reason": rep.stopReason, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": rep.usage.OutputTokens}}); err != nil {
		return err
	}
	return out.event("message_stop", map[string]any{"type": "message_stop"})
}
