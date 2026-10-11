package rates

import (
	"context"
	"fmt"
)

// Move points e at toMatterID and toClientID. A manual (tier 1) rate is kept and
// RateRecheck is set so a person with time.rates confirms it; any other rate is re-resolved
// through tiers 2..8 for the new matter.
func (s *Store) Move(ctx context.Context, e Entry, toMatterID, toClientID string) (Entry, error) {
	e.Input.MatterID, e.Input.ClientID = toMatterID, toClientID
	// rule: BIL-C-005
	if e.Resolved.Source == SourceManual {
		e.RateRecheck = true
		return e, nil
	}
	in := e.Input
	in.ManualRateCents = nil
	res, err := s.Resolve(ctx, in)
	if err != nil {
		return Entry{}, fmt.Errorf("move %s: %w", e.ID, err)
	}
	e.Resolved = res
	return e, nil
}
