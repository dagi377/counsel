package rates

import (
	"context"
	"fmt"
)

// Reprice adds row, then re-resolves every unbilled entry dated on or after
// row.EffectiveFrom and returns the entries whose resolved rate changed. Billed entries,
// entries dated before the change and manual (tier 1) rates are left as they are.
func (s *Store) Reprice(ctx context.Context, row RateRow, entries []Entry) ([]Entry, error) {
	if err := s.Put(ctx, row); err != nil {
		return nil, fmt.Errorf("reprice: %w", err)
	}
	var changed []Entry
	for _, e := range entries {
		// rule: BIL-C-004
		if e.Billed || e.Input.WorkDate.Before(row.EffectiveFrom) || e.Resolved.Source == SourceManual {
			continue
		}
		in := e.Input
		in.ManualRateCents = nil
		res, err := s.Resolve(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("reprice %s: %w", e.ID, err)
		}
		if res == e.Resolved {
			continue
		}
		e.Resolved = res
		changed = append(changed, e)
	}
	return changed, nil
}
