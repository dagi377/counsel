package sync

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Polling cadence and overlap (MIG-043).
const (
	PollIntervalOffPeak = 15 * time.Minute
	PollIntervalPeak    = 60 * time.Minute
	PollOverlap         = 10 * time.Minute
)

// ErrBadRecord is returned for a polled record with no resource, id or update time.
var ErrBadRecord = errors.New("clio/sync: polled record lacks resource, id or updated_at")

// SourceRecord is one record version as seen in the source.
type SourceRecord struct {
	Resource  string
	ID        string
	ETag      string
	UpdatedAt time.Time
}

// ChangeSource lists records of a resource updated at or after since.
type ChangeSource interface {
	Changed(ctx context.Context, resource string, since time.Time) ([]SourceRecord, error)
}

// SyncStore holds the stored version of each record, one row per (resource, id).
type SyncStore interface {
	// Stored returns the stored version, or ok=false when the record is new.
	Stored(ctx context.Context, resource, id string) (rec SourceRecord, ok bool, err error)
	// Apply inserts or replaces the row for (resource, id).
	Apply(ctx context.Context, rec SourceRecord) error
}

// PollResult counts what one poll did.
type PollResult struct {
	Since   time.Time
	Seen    int
	Applied int
	Ignored int
}

// Poller fills gaps that webhooks missed by polling the source.
type Poller struct {
	Source ChangeSource
	Store  SyncStore
}

// PollInterval is the polling cadence: hourly at peak, every 15 minutes off-peak.
func PollInterval(peak bool) time.Duration {
	// rule: MIG-043
	if peak {
		return PollIntervalPeak
	}
	return PollIntervalOffPeak
}

// PollSince is the updated_since for the next poll: the last watermark minus the overlap,
// or, after a webhook lapse, the last delivered event time minus the overlap when earlier.
// A zero lastDelivered means no lapse.
func PollSince(watermark, lastDelivered time.Time) time.Time {
	// rule: MIG-043
	from := watermark
	if !lastDelivered.IsZero() && (from.IsZero() || lastDelivered.Before(from)) {
		from = lastDelivered
	}
	return from.Add(-PollOverlap)
}

// Newer reports whether got should replace stored: only a strictly later updated_at wins,
// so an older or equal version is ignored.
func Newer(got, stored SourceRecord) bool {
	// rule: MIG-044
	return got.UpdatedAt.After(stored.UpdatedAt)
}

// Poll reads resource changes since `since` and applies each record that is new or newer
// than the stored row. Overlapping polls never create duplicate rows.
func (p *Poller) Poll(ctx context.Context, resource string, since time.Time) (PollResult, error) {
	res := PollResult{Since: since}
	recs, err := p.Source.Changed(ctx, resource, since)
	if err != nil {
		return res, fmt.Errorf("poll %s: %w", resource, err)
	}
	for _, r := range recs {
		if r.Resource == "" || r.ID == "" || r.UpdatedAt.IsZero() {
			return res, fmt.Errorf("poll %s: %w", resource, ErrBadRecord)
		}
		res.Seen++
		stored, ok, err := p.Store.Stored(ctx, r.Resource, r.ID)
		if err != nil {
			return res, fmt.Errorf("poll %s %s: %w", resource, r.ID, err)
		}
		// rule: MIG-044
		if ok && !Newer(r, stored) {
			res.Ignored++
			continue
		}
		if err := p.Store.Apply(ctx, r); err != nil {
			return res, fmt.Errorf("poll %s %s: apply: %w", resource, r.ID, err)
		}
		res.Applied++
	}
	return res, nil
}
