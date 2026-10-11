// CONTRACT: the request, response and stream types in this file are frozen
// (spec/ai/mocks.md sec. 2). Changes are additive only and need a docs/QUESTIONS.md entry.

package ai

import "encoding/json"

// CallKind is the kind of model call a feature makes (features.json model_call).
type CallKind string // chat | tool-use | extraction | embedding (features.json model_call)

// Call kinds.
const (
	KindChat       CallKind = "chat"
	KindToolUse    CallKind = "tool-use"
	KindExtraction CallKind = "extraction"
	KindEmbedding  CallKind = "embedding"
)

// Request is one model call.
type Request struct {
	FeatureID   string // "F-TIM-01"; required, keys fixtures, usage and cost
	Kind        CallKind
	Model       string // logical name from config ("drafting", "fast"); never a vendor id in code
	System      string
	Messages    []Message
	Tools       []ToolSpec      // from kernel/agenttools registry; JSON Schema input
	ToolChoice  string          // "auto" | "none" | "<tool name>"
	Schema      json.RawMessage // extraction: JSON Schema the output must satisfy
	MaxTokens   int
	Temperature *float64
	Metadata    map[string]string // run_id, acting_for; never client content
}

// Message is one turn of the conversation sent to the model.
type Message struct {
	Role    string         // "user" | "assistant" | "tool"
	Content []ContentBlock // text, tool_use, tool_result; outside text arrives as quoted tool_result data (ARC-I-038)
}

// ContentBlock is one piece of a message.
type ContentBlock struct {
	Type      string // "text" | "tool_use" | "tool_result"
	Text      string
	ToolUseID string
	ToolName  string
	Input     json.RawMessage
	IsError   bool
}

// ToolSpec describes one tool the model may call.
type ToolSpec struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// ToolCall is one tool call the model asked for.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// Response is the result of one non-streaming call, or the final event of a stream.
type Response struct {
	Text       string
	ToolCalls  []ToolCall
	JSON       json.RawMessage // extraction output, validated against Request.Schema
	StopReason string          // "end_turn" | "tool_use" | "max_tokens" | "stop_sequence"
	Usage      Usage
	Fixture    string // mock only: fixture path that answered; empty in live mode
}

// Usage is the token and cost count of one call.
type Usage struct {
	InputTokens, OutputTokens, CacheReadTokens int
	CostMicroCents                             int64
}

// StreamEvent is one event of a streaming call. Exactly one field is set.
type StreamEvent struct {
	TextDelta     string
	ToolCallDelta *ToolCall // Input holds a partial JSON fragment
	Done          *Response
	Err           error
}

// EmbedRequest asks for one vector per input.
type EmbedRequest struct {
	FeatureID string
	Inputs    []string
}

// EmbedResponse holds one vector per input, in input order.
type EmbedResponse struct {
	Vectors [][]float32
	Usage   Usage
}
