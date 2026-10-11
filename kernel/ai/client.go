// CONTRACT: the ModelClient and Embedder interfaces, Mode and the sentinel errors in this
// file are frozen (spec/ai/mocks.md sec. 2). Changes are additive only and need a
// docs/QUESTIONS.md entry first.

// Package ai is the only path from Counsel code to a language model.
package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ModelClient sends one model request. Implementations: httpclient (mock or live base URL)
// and aifake (in-process). Feature code depends on this interface only.
type ModelClient interface {
	// Complete runs one non-streaming request. Tool calls come back in Response.ToolCalls;
	// the caller runs them through kernel/agenttools and calls Complete again.
	Complete(ctx context.Context, req Request) (Response, error)
	// Stream runs one streaming request. The channel closes after a Done or Err event.
	Stream(ctx context.Context, req Request) (<-chan StreamEvent, error)
	// Mode reports "mock" or "live" (shown on /healthz).
	Mode() Mode
}

// Embedder turns text into vectors (ranking, dedupe, related records, search).
type Embedder interface {
	Embed(ctx context.Context, req EmbedRequest) (EmbedResponse, error)
}

// Mode says whether model calls go to a mock or a live provider.
type Mode string

const (
	ModeMock Mode = "mock"
	ModeLive Mode = "live"
)

// Sentinel errors (owned here).
var (
	ErrNoFeature          = errors.New("ai: request has no feature id")
	ErrNoFixture          = errors.New("ai: no fixture for feature and prompt hash")
	ErrRateLimited        = errors.New("ai: rate limited") // wraps retry-after
	ErrOverloaded         = errors.New("ai: provider overloaded")
	ErrTimeout            = errors.New("ai: model timeout")
	ErrBadOutput          = errors.New("ai: output failed schema")
	ErrLiveModelForbidden = errors.New("ai: live model forbidden in this environment")
)

// ErrUnknownMode is returned by ParseMode for a value that is neither mock nor live.
var ErrUnknownMode = errors.New("ai: unknown model mode")

// ParseMode reads a model.mode config value. An empty value is ModeMock: a live model runs
// only when someone opts in explicitly.
func ParseMode(s string) (Mode, error) {
	// rule: AI-R-001
	switch Mode(strings.TrimSpace(s)) {
	case "", ModeMock:
		return ModeMock, nil
	case ModeLive:
		return ModeLive, nil
	default:
		return "", fmt.Errorf("parse model mode %q: %w", s, ErrUnknownMode)
	}
}

// Validate checks the fields every model request must carry.
func (r Request) Validate() error {
	// rule: AI-R-005
	if strings.TrimSpace(r.FeatureID) == "" {
		return ErrNoFeature
	}
	return nil
}

// Validate checks the fields every embedding request must carry.
func (r EmbedRequest) Validate() error {
	// rule: AI-R-005
	if strings.TrimSpace(r.FeatureID) == "" {
		return ErrNoFeature
	}
	return nil
}
