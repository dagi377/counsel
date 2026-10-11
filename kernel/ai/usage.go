package ai

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

// UsageRecord is what Counsel keeps about one model call. It holds identifiers and counts
// only: no prompt, completion, tool input or metadata value is ever part of it.
type UsageRecord struct {
	FeatureID string
	Kind      CallKind
	Model     string // logical model name from config
	Mode      Mode
	Usage     Usage
}

// UsageRecorder stores usage records (in memory, or a table owned by the recorder's module).
type UsageRecorder interface {
	Record(ctx context.Context, rec UsageRecord) error
}

// Add returns the sum of two usage counts.
func (u Usage) Add(o Usage) Usage {
	return Usage{
		InputTokens:     u.InputTokens + o.InputTokens,
		OutputTokens:    u.OutputTokens + o.OutputTokens,
		CacheReadTokens: u.CacheReadTokens + o.CacheReadTokens,
		CostMicroCents:  u.CostMicroCents + o.CostMicroCents,
	}
}

// FeatureUsage is the running total of one feature.
type FeatureUsage struct {
	FeatureID string
	Calls     int
	Usage     Usage
}

// UsageLedger is an in-memory UsageRecorder that totals usage per feature id.
// It is safe for concurrent use. The zero value is ready to use.
type UsageLedger struct {
	mu     sync.Mutex
	totals map[string]FeatureUsage
}

// Record adds rec to its feature's total.
func (l *UsageLedger) Record(ctx context.Context, rec UsageRecord) error {
	// rule: AI-R-005
	if rec.FeatureID == "" {
		return fmt.Errorf("record usage: %w", ErrNoFeature)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.totals == nil {
		l.totals = make(map[string]FeatureUsage)
	}
	t := l.totals[rec.FeatureID]
	t.FeatureID = rec.FeatureID
	t.Calls++
	t.Usage = t.Usage.Add(rec.Usage)
	l.totals[rec.FeatureID] = t
	return nil
}

// Feature returns the total for one feature id; zero counts when it has no calls.
func (l *UsageLedger) Feature(featureID string) FeatureUsage {
	l.mu.Lock()
	defer l.mu.Unlock()
	t, ok := l.totals[featureID]
	if !ok {
		return FeatureUsage{FeatureID: featureID}
	}
	return t
}

// All returns every feature total, sorted by feature id.
func (l *UsageLedger) All() []FeatureUsage {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]FeatureUsage, 0, len(l.totals))
	for _, t := range l.totals {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FeatureID < out[j].FeatureID })
	return out
}

// WithUsage wraps a ModelClient so every request must carry a feature id and every
// answered call is recorded per feature id.
func WithUsage(next ModelClient, rec UsageRecorder) ModelClient {
	return &usageClient{next: next, rec: rec}
}

// WithEmbedUsage wraps an Embedder the same way as WithUsage.
func WithEmbedUsage(next Embedder, rec UsageRecorder, mode Mode) Embedder {
	return &usageEmbedder{next: next, rec: rec, mode: mode}
}

type usageClient struct {
	next ModelClient
	rec  UsageRecorder
}

func (c *usageClient) Mode() Mode { return c.next.Mode() }

func (c *usageClient) Complete(ctx context.Context, req Request) (Response, error) {
	// rule: AI-R-005
	if err := req.Validate(); err != nil {
		return Response{}, err
	}
	resp, err := c.next.Complete(ctx, req)
	if err != nil {
		return resp, fmt.Errorf("complete %s: %w", req.FeatureID, err)
	}
	record(ctx, c.rec, recordFor(req, c.next.Mode(), resp.Usage))
	return resp, nil
}

func (c *usageClient) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	// rule: AI-R-005
	if err := req.Validate(); err != nil {
		return nil, err
	}
	in, err := c.next.Stream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("stream %s: %w", req.FeatureID, err)
	}
	out := make(chan StreamEvent)
	mode := c.next.Mode()
	go func() {
		defer close(out)
		for ev := range in {
			if ev.Done != nil {
				record(ctx, c.rec, recordFor(req, mode, ev.Done.Usage))
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				// Drain so the inner stream can finish and close.
				for range in {
				}
				return
			}
		}
	}()
	return out, nil
}

type usageEmbedder struct {
	next Embedder
	rec  UsageRecorder
	mode Mode
}

func (e *usageEmbedder) Embed(ctx context.Context, req EmbedRequest) (EmbedResponse, error) {
	// rule: AI-R-005
	if err := req.Validate(); err != nil {
		return EmbedResponse{}, err
	}
	resp, err := e.next.Embed(ctx, req)
	if err != nil {
		return resp, fmt.Errorf("embed %s: %w", req.FeatureID, err)
	}
	record(ctx, e.rec, UsageRecord{FeatureID: req.FeatureID, Kind: KindEmbedding, Mode: e.mode, Usage: resp.Usage})
	return resp, nil
}

func recordFor(req Request, mode Mode, u Usage) UsageRecord {
	// rule: AI-R-005
	return UsageRecord{FeatureID: req.FeatureID, Kind: req.Kind, Model: req.Model, Mode: mode, Usage: u}
}

// record stores rec. A failure is logged and does not fail the call: the model already
// answered, and failing here would make the caller retry and pay twice.
func record(ctx context.Context, rec UsageRecorder, r UsageRecord) {
	if rec == nil {
		return
	}
	if err := rec.Record(ctx, r); err != nil {
		slog.WarnContext(ctx, "ai usage not recorded", "feature", r.FeatureID, "err", err)
	}
}
