package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/dagi377/counsel/kernel/ai"
)

// OpenAI-compatible Chat Completions protocol: POST /v1/chat/completions. Requests map onto the
// same kernel/ai types as /v1/messages, so one fixture answers all protocols.

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []chatMessage  `json:"messages"`
	Tools    []chatTool     `json:"tools"`
	Stream   bool           `json:"stream"`
	Metadata map[string]any `json:"metadata"`
}

type chatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"` // a string, a list of parts, or null
	ToolCalls  []chatToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	Index    *int         `json:"index,omitempty"` // stream deltas only
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

type chatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatChoice struct {
	Index        int          `json:"index"`
	Message      *chatMessage `json:"message,omitempty"`
	Delta        *chatDelta   `json:"delta,omitempty"`
	FinishReason *string      `json:"finish_reason"`
}

type chatDelta struct {
	Role      string         `json:"role,omitempty"`
	Content   *string        `json:"content,omitempty"`
	ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}

// textParts decodes an OpenAI-style content field: a bare string, null, or a list of parts
// whose "text" fields are joined.
func textParts(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", fmt.Errorf("content string: %w", err)
		}
		return s, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", fmt.Errorf("content parts: %w", err)
	}
	var out []string
	for _, p := range parts {
		if p.Text != "" {
			out = append(out, p.Text)
		}
	}
	return strings.Join(out, "\n"), nil
}

// appendBlock adds b to the last message when it has role, else starts a new message, so
// consecutive tool calls or tool results group the way the Messages protocol groups them.
func appendBlock(msgs []ai.Message, role string, b ai.ContentBlock) []ai.Message {
	if n := len(msgs); n > 0 && msgs[n-1].Role == role && b.Type != "text" && len(msgs[n-1].Content) > 0 && msgs[n-1].Content[len(msgs[n-1].Content)-1].Type == b.Type {
		msgs[n-1].Content = append(msgs[n-1].Content, b)
		return msgs
	}
	return append(msgs, ai.Message{Role: role, Content: []ai.ContentBlock{b}})
}

// rawArgs turns an OpenAI arguments string into the JSON input of a tool_use block.
func rawArgs(s string) json.RawMessage {
	if strings.TrimSpace(s) == "" {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(s)
}

// identify fills the feature and run id of c: header, else metadata, else the system prompt.
func identify(c *call, r *http.Request, meta map[string]any) {
	c.feature = r.Header.Get("X-Counsel-Feature")
	if c.feature == "" {
		c.feature = metaString(meta, "feature")
	}
	if c.feature == "" {
		c.feature = featureFromSystem(c.system)
	}
	c.runID = r.Header.Get("X-Counsel-Run")
	if c.runID == "" {
		c.runID = metaString(meta, "run_id")
	}
	if c.runID == "" {
		c.runID = runFromSystem(c.system)
	}
}

func (q chatRequest) toCall(r *http.Request) (call, error) {
	var c call
	var system []string
	for i, m := range q.Messages {
		text, err := textParts(m.Content)
		if err != nil {
			return call{}, fmt.Errorf("messages[%d]: %w", i, err)
		}
		switch m.Role {
		case "system", "developer":
			system = append(system, text)
		case "user":
			c.messages = append(c.messages, ai.Message{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: text}}})
		case "assistant":
			msg := ai.Message{Role: "assistant"}
			if text != "" {
				msg.Content = append(msg.Content, ai.ContentBlock{Type: "text", Text: text})
			}
			for _, tc := range m.ToolCalls {
				msg.Content = append(msg.Content, ai.ContentBlock{Type: "tool_use", ToolUseID: tc.ID, ToolName: tc.Function.Name, Input: rawArgs(tc.Function.Arguments)})
			}
			c.messages = append(c.messages, msg)
		case "tool":
			c.messages = appendBlock(c.messages, "user", ai.ContentBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Text: text})
		default:
			return call{}, fmt.Errorf("messages[%d]: unknown role %q", i, m.Role)
		}
	}
	c.system = strings.Join(system, "\n")
	for _, t := range q.Tools {
		c.tools = append(c.tools, ai.ToolSpec{Name: t.Function.Name, Description: t.Function.Description, InputSchema: t.Function.Parameters})
	}
	identify(&c, r, q.Metadata)
	return c, nil
}

// writeOpenAIError renders e in the OpenAI-compatible error shape (Chat Completions and Responses).
func writeOpenAIError(w http.ResponseWriter, e *apiError) {
	body := map[string]any{"message": e.message, "type": e.typ, "code": e.typ, "param": nil}
	for k, v := range e.extra {
		body[k] = v
	}
	writeJSON(w, e.status, map[string]any{"error": body})
}

// finishReason maps a Messages stop reason onto a Chat Completions finish_reason.
func finishReason(stop string) string {
	switch stop {
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	default:
		return "stop"
	}
}

func (rep reply) chatUsage() *chatUsage {
	return &chatUsage{PromptTokens: rep.usage.InputTokens, CompletionTokens: rep.usage.OutputTokens, TotalTokens: rep.usage.InputTokens + rep.usage.OutputTokens}
}

func (rep reply) chatToolCalls() []chatToolCall {
	var out []chatToolCall
	for _, tc := range rep.toolCalls {
		out = append(out, chatToolCall{ID: tc.ID, Type: "function", Function: chatFunction{Name: tc.Name, Arguments: string(tc.Input)}})
	}
	return out
}

// created is the fixed creation time of every completion, so bodies are byte-identical across runs.
func created() int64 {
	t, _ := time.Parse(time.RFC3339, modelsCreated)
	return t.Unix()
}

// chatCompletions answers POST /v1/chat/completions from a fixture; nothing here can reach a
// real model.
// rule: AI-R-001
func (s *server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	var q chatRequest
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
		slog.Info("llm fake: call refused", "protocol", "chat", "feature", c.feature, "status", e.status, "type", e.typ)
		writeOpenAIError(w, e)
		return
	}
	rep := buildReply(fx, t, n, c)
	slog.Info("llm fake: call answered", "protocol", "chat", "feature", c.feature, "fixture", rep.fixture, "turn", n, "stream", q.Stream)
	if q.Stream {
		if err := rep.chatStream(w, q.Model); err != nil {
			slog.Warn("llm fake: stream", "feature", c.feature, "err", err)
		}
		return
	}
	finish := finishReason(rep.stopReason)
	msg := &chatMessage{Role: "assistant", Content: json.RawMessage("null"), ToolCalls: rep.chatToolCalls()}
	if rep.text != "" {
		b, _ := json.Marshal(rep.text)
		msg.Content = b
	}
	writeJSON(w, http.StatusOK, chatResponse{ID: "chatcmpl-" + rep.id, Object: "chat.completion", Created: created(), Model: q.Model,
		Choices: []chatChoice{{Index: 0, Message: msg, FinishReason: &finish}}, Usage: rep.chatUsage()})
}

// chatStream renders rep as Chat Completions chunks: a role chunk, text deltas word by word,
// tool call deltas (id and name, then argument fragments), a finish chunk, a usage chunk with
// no choices, then "data: [DONE]".
func (rep reply) chatStream(w http.ResponseWriter, model string) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	send := func(choices []chatChoice, usage *chatUsage) error {
		if choices == nil {
			choices = []chatChoice{}
		}
		b, err := json.Marshal(chatResponse{ID: "chatcmpl-" + rep.id, Object: "chat.completion.chunk", Created: created(), Model: model, Choices: choices, Usage: usage})
		if err != nil {
			return fmt.Errorf("encode chunk: %w", err)
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return fmt.Errorf("write chunk: %w", err)
		}
		if fl != nil {
			fl.Flush()
		}
		return nil
	}
	delta := func(d chatDelta) error { return send([]chatChoice{{Index: 0, Delta: &d}}, nil) }

	empty := ""
	if err := delta(chatDelta{Role: "assistant", Content: &empty}); err != nil {
		return err
	}
	for _, word := range strings.SplitAfter(rep.text, " ") {
		if word == "" {
			continue
		}
		if err := delta(chatDelta{Content: &word}); err != nil {
			return err
		}
	}
	for i, tc := range rep.toolCalls {
		idx := i
		if err := delta(chatDelta{ToolCalls: []chatToolCall{{Index: &idx, ID: tc.ID, Type: "function", Function: chatFunction{Name: tc.Name}}}}); err != nil {
			return err
		}
		for _, part := range chunkJSON(string(tc.Input), jsonChunk) {
			if err := delta(chatDelta{ToolCalls: []chatToolCall{{Index: &idx, Function: chatFunction{Arguments: part}}}}); err != nil {
				return err
			}
		}
	}
	finish := finishReason(rep.stopReason)
	if err := send([]chatChoice{{Index: 0, Delta: &chatDelta{}, FinishReason: &finish}}, nil); err != nil {
		return err
	}
	if err := send(nil, rep.chatUsage()); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
		return fmt.Errorf("write done: %w", err)
	}
	if fl != nil {
		fl.Flush()
	}
	return nil
}
