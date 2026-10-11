// Package ratesfake is an in-memory rates.Service for tests of other modules.
package ratesfake

import (
	"context"
	"fmt"
	"github.com/dagi377/counsel/kernel/clock"
	"sync"

	"github.com/dagi377/counsel/app/legal/timekeeping/rates"
)

var _ rates.Service = (*Fake)(nil)

// Fake keeps rate rows in memory and resolves them through rates.Chain.
type Fake struct {
	mu   sync.Mutex
	rows []rates.RateRow
}

// New returns an empty Fake.
func New() *Fake { return &Fake{} }

// Put adds row.
func (f *Fake) Put(_ context.Context, row rates.RateRow) error {
	if row.Source == rates.SourceManual || row.RateCents < 0 || !hasKeys(row) {
		return fmt.Errorf("put %s: %w", row.Source, rates.ErrBadRow)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, row)
	return nil
}

// Resolve returns the first tier that hits, using the latest row effective on the work date.
func (f *Fake) Resolve(_ context.Context, in rates.Input) (rates.Resolved, error) {
	if in.ManualRateCents != nil {
		if !in.CanSetRates {
			return rates.Resolved{}, rates.ErrManualRateForbidden
		}
		return rates.Resolved{RateCents: *in.ManualRateCents, Source: rates.SourceManual}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, src := range rates.Chain[1:] {
		var best *rates.RateRow
		for i := range f.rows {
			r := &f.rows[i]
			if r.Source != src || !matches(*r, in) || after(r.EffectiveFrom, in.WorkDate) {
				continue
			}
			if best == nil || after(r.EffectiveFrom, best.EffectiveFrom) {
				best = r
			}
		}
		if best != nil {
			return rates.Resolved{RateCents: best.RateCents, Source: src}, nil
		}
	}
	return rates.Resolved{}, rates.ErrNoRate
}

// BilledMinutes rounds raw minutes and applies the minimum entry.
func (f *Fake) BilledMinutes(raw int, r rates.Rounding) (int, error) {
	if (r.IncrementMinutes != 1 && r.IncrementMinutes != 6 && r.IncrementMinutes != 15) ||
		(r.Mode != rates.ModeUp && r.Mode != rates.ModeNearest) || raw < 0 {
		return 0, rates.ErrBadRounding
	}
	inc := r.IncrementMinutes
	billed := (raw + inc - 1) / inc * inc
	if r.Mode == rates.ModeNearest {
		billed = (2*raw + inc) / (2 * inc) * inc
	}
	if raw > 0 && billed < r.MinimumEntry {
		billed = r.MinimumEntry
	}
	return billed, nil
}

// Amount is billedMinutes * rateCents / 60, rounded half-up.
func (f *Fake) Amount(billedMinutes int, rateCents int64) (int64, error) {
	n := int64(billedMinutes) * rateCents
	if n < 0 {
		return 0, fmt.Errorf("amount: negative product")
	}
	return (2*n + 60) / 120, nil
}

// Reprice adds row, then re-resolves unbilled entries dated on or after its effective date.
func (f *Fake) Reprice(ctx context.Context, row rates.RateRow, entries []rates.Entry) ([]rates.Entry, error) {
	if err := f.Put(ctx, row); err != nil {
		return nil, err
	}
	var changed []rates.Entry
	for _, e := range entries {
		if e.Billed || after(row.EffectiveFrom, e.Input.WorkDate) || e.Resolved.Source == rates.SourceManual {
			continue
		}
		res, err := f.Resolve(ctx, e.Input)
		if err != nil {
			return nil, fmt.Errorf("reprice %s: %w", e.ID, err)
		}
		if res != e.Resolved {
			e.Resolved = res
			changed = append(changed, e)
		}
	}
	return changed, nil
}

// Move re-resolves e on the new matter; a manual rate is kept and flagged for recheck.
func (f *Fake) Move(ctx context.Context, e rates.Entry, toMatterID, toClientID string) (rates.Entry, error) {
	e.Input.MatterID, e.Input.ClientID = toMatterID, toClientID
	if e.Resolved.Source == rates.SourceManual {
		e.RateRecheck = true
		return e, nil
	}
	in := e.Input
	in.ManualRateCents = nil
	res, err := f.Resolve(ctx, in)
	if err != nil {
		return e, fmt.Errorf("move %s: %w", e.ID, err)
	}
	e.Resolved = res
	return e, nil
}

func hasKeys(r rates.RateRow) bool {
	switch r.Source {
	case rates.SourceFlatActivity, rates.SourceActivity:
		return r.ActivityID != ""
	case rates.SourceMatterUser:
		return r.MatterID != "" && r.UserID != ""
	case rates.SourceMatterRole:
		return r.MatterID != "" && r.RoleGroup != ""
	case rates.SourceClientUser:
		return r.ClientID != "" && r.UserID != ""
	case rates.SourceClientRole:
		return r.ClientID != "" && r.RoleGroup != ""
	case rates.SourceUserDefault:
		return r.UserID != ""
	}
	return false
}

func matches(r rates.RateRow, in rates.Input) bool {
	switch r.Source {
	case rates.SourceFlatActivity, rates.SourceActivity:
		return r.ActivityID == in.ActivityID
	case rates.SourceMatterUser:
		return r.MatterID == in.MatterID && r.UserID == in.UserID
	case rates.SourceMatterRole:
		return r.MatterID == in.MatterID && r.RoleGroup == in.RoleGroup
	case rates.SourceClientUser:
		return r.ClientID == in.ClientID && r.UserID == in.UserID
	case rates.SourceClientRole:
		return r.ClientID == in.ClientID && r.RoleGroup == in.RoleGroup
	case rates.SourceUserDefault:
		return r.UserID == in.UserID
	}
	return false
}

func after(a, b clock.Date) bool { return b.Before(a) }
