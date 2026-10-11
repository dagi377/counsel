package rates

import (
	"context"
	"fmt"
	"sync"

	"github.com/dagi377/counsel/kernel/money"
)

// Store keeps effective-dated rate rows in memory and resolves them through Chain.
// It implements Resolver and Table. Divisions go through the injected money.Arithmetic.
type Store struct {
	mu   sync.Mutex
	rows []RateRow
	m    money.Arithmetic
}

var (
	_ Resolver = (*Store)(nil)
	_ Table    = (*Store)(nil)
)

// NewStore returns an empty Store that divides with m.
func NewStore(m money.Arithmetic) *Store {
	return &Store{m: m}
}

// Put adds row. A manual source, a missing key or a negative rate returns ErrBadRow.
func (s *Store) Put(_ context.Context, row RateRow) error {
	if row.Source == SourceManual || row.RateCents < 0 || !rowKeyed(row) {
		return fmt.Errorf("put %s: %w", row.Source, ErrBadRow)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, row)
	return nil
}

// Resolve returns the first tier in Chain that has a row effective on in.WorkDate. Within a
// tier the row with the latest EffectiveFrom on or before the work date wins.
func (s *Store) Resolve(_ context.Context, in Input) (Resolved, error) {
	// rule: BIL-C-003
	if in.ManualRateCents != nil {
		if !in.CanSetRates {
			return Resolved{}, fmt.Errorf("resolve: %w", ErrManualRateForbidden)
		}
		return Resolved{RateCents: *in.ManualRateCents, Source: SourceManual}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// rule: BIL-C-003
	for _, src := range Chain[1:] {
		var best *RateRow
		for i := range s.rows {
			r := &s.rows[i]
			if r.Source != src || !rowMatches(*r, in) || in.WorkDate.Before(r.EffectiveFrom) {
				continue
			}
			if best == nil || best.EffectiveFrom.Before(r.EffectiveFrom) {
				best = r
			}
		}
		if best != nil {
			return Resolved{RateCents: best.RateCents, Source: src}, nil
		}
	}
	return Resolved{}, fmt.Errorf("resolve: %w", ErrNoRate)
}

// Amount is the time line amount, rhu(billedMinutes * rateCents, 60).
func (s *Store) Amount(billedMinutes int, rateCents int64) (int64, error) {
	if billedMinutes < 0 || rateCents < 0 {
		return 0, fmt.Errorf("amount: %w", ErrBadRow)
	}
	// rule: BIL-C-011
	a, err := s.m.Rhu(int64(billedMinutes)*rateCents, 60)
	if err != nil {
		return 0, fmt.Errorf("amount: %w", err)
	}
	return a, nil
}

// applyMinimum lifts billed to the minimum entry when any time was recorded.
func applyMinimum(raw, billed, minimum int) int {
	// rule: BIL-C-012
	if raw > 0 && billed < minimum {
		return minimum
	}
	return billed
}

func rowKeyed(r RateRow) bool {
	switch r.Source {
	case SourceFlatActivity, SourceActivity:
		return r.ActivityID != ""
	case SourceMatterUser:
		return r.MatterID != "" && r.UserID != ""
	case SourceMatterRole:
		return r.MatterID != "" && r.RoleGroup != ""
	case SourceClientUser:
		return r.ClientID != "" && r.UserID != ""
	case SourceClientRole:
		return r.ClientID != "" && r.RoleGroup != ""
	case SourceUserDefault:
		return r.UserID != ""
	}
	return false
}

func rowMatches(r RateRow, in Input) bool {
	switch r.Source {
	case SourceFlatActivity, SourceActivity:
		return r.ActivityID == in.ActivityID
	case SourceMatterUser:
		return r.MatterID == in.MatterID && r.UserID == in.UserID
	case SourceMatterRole:
		return r.MatterID == in.MatterID && r.RoleGroup == in.RoleGroup
	case SourceClientUser:
		return r.ClientID == in.ClientID && r.UserID == in.UserID
	case SourceClientRole:
		return r.ClientID == in.ClientID && r.RoleGroup == in.RoleGroup
	case SourceUserDefault:
		return r.UserID == in.UserID
	}
	return false
}
