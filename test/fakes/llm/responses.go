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

// OpenAI-compatible Responses protocol: POST /v1/responses. Input items map onto the same
// kernel/ai types as /v1/messages, so one fixture answers all protocols.

type responsesRequest struct {
	Model        string          `json:"model"`
	Instructions string          `json:"instructions"`
	Input        json.RawMessage `json:"input"` // a string or a list of items
	Tools        []responsesTool `json:"tools"`
	Stream       bool            `json:"stream"`
	Metadata     map[string]any  `json:"metadata"`
}

type responsesItem struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	CallID  string          `json:"call_id"`
	Name    string          `json:"name"`
	Args    string          `json:"arguments"`
	Output  json.RawMessage `json:"output"` // a string or a list of parts
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type responsesPart struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
}

type responsesOutput struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Status    string          `json:"status"`
	Role      string          `json:"role,omitempty"`
	Content   []responsesPart `json:"content,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments *string         `json:"arguments,omitempty"`
}

type responsesResponse struct {
	ID                string            `json:"id"`
	Object            string            `json:"object"`
	CreatedAt         int64             `json:"created_at"`
	Status            string            `json:"status"`
	IncompleteDetails map[string]string `json:"incomplete_details"`
	Model             string            `json:"model"`
	Output            []responsesOutput `json:"output"`
	Usage             *responsesUsage   `json:"usage"`
}

func (q responsesRequest) toCall(r *http.Request) (call, error) {
	c := call{system: q.Instructions}
	raw := bytes.TrimSpace(q.Input)
	var items []responsesItem
	switch {
	case len(raw) == 0 || string(raw) == "null":
	case raw[0] == '"':
		items = []responsesItem{{Type: "message", Role: "user", Content: raw}}
	default:
		if err := json.Unmarshal(raw, &items); err != nil {
			return call{}, fmt.Errorf("input items: %w", err)
		}
	}
	var system []string
	if q.Instructions != "" {
		system = append(system, q.Instructions)
	}
	for i, it := range items {
		switch it.Type {
		case "", "message":
			text, err := textParts(it.Content)
			if err != nil {
				return call{}, fmt.Errorf("input[%d]: %w", i, err)
			}
			switch it.Role {
			case "system", "developer":
				system = append(system, text)
			case "user", "assistant":
				c.messages = append(c.messages, ai.Message{Role: it.Role, Content: []ai.ContentBlock{{Type: "text", Text: text}}})
			default:
				return call{}, fmt.Errorf("input[%d]: unknown role %q", i, it.Role)
			}
		case "function_call":
			c.messages = appendBlock(c.messages, "assistant", ai.ContentBlock{Type: "tool_use", ToolUseID: it.CallID, ToolName: it.Name, Input: rawArgs(it.Args)})
		case "function_call_output":
			out, err := textParts(it.Output)
			if err != nil {
				return call{}, fmt.Errorf("input[%d] output: %w", i, err)
			}
			c.messages = appendBlock(c.messages, "user", ai.ContentBlock{Type: "tool_result", ToolUseID: it.CallID, Text: out})
		default:
			return call{}, fmt.Errorf("input[%d]: unsupported item type %q", i, it.Type)
		}
	}
	c.system = strings.Join(system, "\n")
	for _, t := range q.Tools {
		c.tools = append(c.tools, ai.ToolSpec{Name: t.Name, Description: t.Description, InputSchema: t.Parameters})
	}
	identify(&c, r, q.Metadata)
	return c, nil
}

// responsesBody renders rep as a Responses object: an output_text message item, then one
// function_call item per tool call.
func (rep reply) responsesBody(model string) responsesResponse {
	out := responsesResponse{ID: "resp_" + rep.id, Object: "response", CreatedAt: created(), Status: "completed", Model: model,
		Output: []responsesOutput{}, Usage: &responsesUsage{InputTokens: rep.usage.InputTokens, OutputTokens: rep.usage.OutputTokens, TotalTokens: rep.usage.InputTokens + rep.usage.OutputTokens}}
	if rep.stopReason == "max_tokens" {
		out.Status, out.IncompleteDetails = "incomplete", map[string]string{"reason": "max_output_tokens"}
	}
	if rep.text != "" {
		out.Output = append(out.Output, responsesOutput{Type: "message", ID: "msg_" + rep.id, Status: "completed", Role: "assistant",
			Content: []responsesPart{{Type: "output_text", Text: rep.text, Annotations: []any{}}}})
	}
	for _, tc := range rep.toolCalls {
		args := string(tc.Input)
		out.Output = append(out.Output, responsesOutput{Type: "function_call", ID: "fc_" + tc.ID, Status: "completed", CallID: tc.ID, Name: tc.Name, Arguments: &args})
	}
	return out
}

// responses answers POST /v1/responses from a fixture; nothing here can reach a real model.
// rule: AI-R-001
func (s *server) responses(w http.ResponseWriter, r *http.Request) {
	var q responsesRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&q); err != nil {
		writeOpenAIError(w, &apiError{status: http.StatusBadRequest, typ: "invalid_request_error", message: "decode body: " + err.Error()})
		return
	}
	c, err := q.toCall(r)
	if err != nil {
		writeOpenAIError(w, &apiError{status: http.StatusBadRequest, typ: "invalid_request_error", message: err.Error()})
		return
	}
	fx, t, n, e := s.route(c)
	if e != nil {
		slog.Info("llm fake: call refused", "protocol", "responses", "feature", c.feature, "status", e.status, "type", e.typ)
		writeOpenAIError(w, e)
		return
	}
	rep := buildReply(fx, t, n, c)
	slog.Info("llm fake: call answered", "protocol", "responses", "feature", c.feature, "fixture", rep.fixture, "turn", n, "stream", q.Stream)
	if q.Stream {
		if err := rep.responsesStream(w, q.Model); err != nil {
			slog.Warn("llm fake: stream", "feature", c.feature, "err", err)
		}
		return
	}
	writeJSON(w, http.StatusOK, rep.responsesBody(q.Model))
}

// responsesStream renders rep as Responses SSE events: response.created, per output item
// output_item.added / text or argument deltas / done events / output_item.done, then
// response.completed carrying the full response and usage.
func (rep reply) responsesStream(w http.ResponseWriter, model string) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	out := sse{w: w, fl: fl}
	seq := 0
	ev := func(name string, v map[string]any) error {
		v["type"], v["sequence_number"] = name, seq
		seq++
		return out.event(name, v)
	}
	full := rep.responsesBody(model)
	pending := full
	pending.Status, pending.Output, pending.Usage, pending.IncompleteDetails = "in_progress", []responsesOutput{}, nil, nil
	if err := ev("response.created", map[string]any{"response": pending}); err != nil {
		return err
	}
	for i, item := range full.Output {
		added := item
		added.Status, added.Content = "in_progress", nil
		if item.Arguments != nil {
			empty := ""
			added.Arguments = &empty
		}
		if err := ev("response.output_item.added", map[string]any{"output_index": i, "item": added}); err != nil {
			return err
		}
		switch item.Type {
		case "message":
			for _, word := range strings.SplitAfter(rep.text, " ") {
				if word == "" {
					continue
				}
				if err := ev("response.output_text.delta", map[string]any{"item_id": item.ID, "output_index": i, "content_index": 0, "delta": word}); err != nil {
					return err
				}
			}
			if err := ev("response.output_text.done", map[string]any{"item_id": item.ID, "output_index": i, "content_index": 0, "text": rep.text}); err != nil {
				return err
			}
		case "function_call":
			for _, part := range chunkJSON(*item.Arguments, jsonChunk) {
				if err := ev("response.function_call_arguments.delta", map[string]any{"item_id": item.ID, "output_index": i, "delta": part}); err != nil {
					return err
				}
			}
			if err := ev("response.function_call_arguments.done", map[string]any{"item_id": item.ID, "output_index": i, "arguments": *item.Arguments}); err != nil {
				return err
			}
		}
		if err := ev("response.output_item.done", map[string]any{"output_index": i, "item": item}); err != nil {
			return err
		}
	}
	done := "response.completed"
	if full.Status == "incomplete" {
		done = "response.incomplete"
	}
	return ev(done, map[string]any{"response": full})
}
