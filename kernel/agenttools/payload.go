package agenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/dagi377/counsel/kernel/registry"
)

// ErrNoPayloadBuilder: submit_for_approval named a gate kind with no payload builder, so no
// gate is opened (fail closed, 422 invalid_payload).
var ErrNoPayloadBuilder = errors.New("agenttools: no payload builder for gate kind")

// ErrPayloadSource: a payload builder could not read a value the gate needs from the
// subject's records (a missing record or a missing field). No gate is opened.
var ErrPayloadSource = errors.New("agenttools: payload source missing")

// PayloadBuilder fills a gate payload for one kind from the subject's stored records. It is
// server-side code: it reads module ports, never the model's text.
type PayloadBuilder func(ctx context.Context, subject registry.Subject) (map[string]any, error)

// PayloadBuilders is the per-kind payload builder registry submit_for_approval reads.
type PayloadBuilders struct {
	mu sync.RWMutex
	m  map[string]PayloadBuilder
}

// NewPayloadBuilders returns an empty registry.
func NewPayloadBuilders() *PayloadBuilders {
	return &PayloadBuilders{m: map[string]PayloadBuilder{}}
}

// Register adds the builder of kind. A nil builder or a second builder for kind is ErrBadTool.
func (b *PayloadBuilders) Register(kind string, f PayloadBuilder) error {
	if kind == "" || f == nil {
		return fmt.Errorf("payload builder %q: %w", kind, ErrBadTool)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.m[kind]; ok {
		return fmt.Errorf("payload builder %q: duplicate: %w", kind, ErrBadTool)
	}
	b.m[kind] = f
	return nil
}

// Kinds lists the kinds that have a builder, sorted.
func (b *PayloadBuilders) Kinds() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return slices.Sorted(maps.Keys(b.m))
}

// Build returns the gate payload of kind for subject: the builder's fields plus the subject
// ids. A kind with no builder is ErrNoPayloadBuilder.
func (b *PayloadBuilders) Build(ctx context.Context, kind string, subject registry.Subject) (json.RawMessage, error) {
	b.mu.RLock()
	f := b.m[kind]
	b.mu.RUnlock()
	// rule: AI-R-003
	// The payload is built from records, so a kind with no builder opens nothing.
	if f == nil {
		return nil, fmt.Errorf("gate kind %q: %w", kind, ErrNoPayloadBuilder)
	}
	fields, err := f(ctx, subject)
	if err != nil {
		return nil, fmt.Errorf("gate kind %q payload: %w", kind, err)
	}
	out := make(map[string]any, len(fields)+2)
	maps.Copy(out, fields)
	out["subject_type"] = subject.Kind
	out["subject_id"] = subject.ID
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("gate kind %q payload: %w", kind, err)
	}
	return raw, nil
}

// submitPayload is the gate payload submit_for_approval opens with. With no registry it is
// the subject ids only (the manual path before builders were wired).
func submitPayload(ctx context.Context, b *PayloadBuilders, kind string, subject registry.Subject) (json.RawMessage, error) {
	if b == nil {
		return json.Marshal(map[string]string{"subject_type": subject.Kind, "subject_id": subject.ID})
	}
	return b.Build(ctx, kind, subject)
}
