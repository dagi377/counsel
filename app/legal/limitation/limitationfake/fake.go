// Package limitationfake is an in-memory limitation.Calculator and limitation.Store for tests.
// Its arithmetic is calendar-only (2 years from discovery, 15 years from the act, extended by
// suspensions); rule-pack day counting is not applied.
package limitationfake

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/dagi377/counsel/app/legal/limitation"
)

var (
	_ limitation.Calculator = (*Fake)(nil)
	_ limitation.Store      = (*Fake)(nil)
)

// Fake keeps assessments in memory. NoPeriod lists claim types with no limitation period.
type Fake struct {
	NoPeriod []string

	mu   sync.Mutex
	rows map[string]limitation.Assessment
	next int
}

// New returns an empty Fake.
func New() *Fake { return &Fake{rows: map[string]limitation.Assessment{}} }

func check(a limitation.Assessment) error {
	if (a.MatterID == "") == (a.LeadID == "") {
		return limitation.ErrOwner
	}
	if a.ActDate != nil && a.DiscoveryDate != nil && a.DiscoveryDate.Before(*a.ActDate) {
		return limitation.ErrDates
	}
	return nil
}

// Compute implements limitation.Calculator.
func (f *Fake) Compute(_ context.Context, a limitation.Assessment) ([]limitation.Period, error) {
	if err := check(a); err != nil {
		return nil, fmt.Errorf("compute: %w", err)
	}
	if slices.Contains(f.NoPeriod, a.ClaimType) {
		return nil, fmt.Errorf("compute %s: %w", a.ClaimType, limitation.ErrNoPeriod)
	}
	if a.ActDate == nil || a.DiscoveryDate == nil {
		return nil, fmt.Errorf("compute: %w", limitation.ErrDates)
	}
	ultimate := a.ActDate.AddDate(15, 0, 0)
	for _, s := range a.Suspensions {
		if s.To == nil {
			return []limitation.Period{{Kind: "basic", Due: a.DiscoveryDate.AddDate(2, 0, 0)}}, nil
		}
		ultimate = ultimate.Add(s.To.Sub(s.From))
	}
	return []limitation.Period{
		{Kind: "basic", Due: a.DiscoveryDate.AddDate(2, 0, 0)},
		{Kind: "ultimate", Due: ultimate},
	}, nil
}

// Create implements limitation.Store.
func (f *Fake) Create(_ context.Context, a limitation.Assessment) (limitation.Assessment, error) {
	if err := check(a); err != nil {
		return limitation.Assessment{}, fmt.Errorf("create assessment: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	a.ID = fmt.Sprintf("la-%d", f.next)
	a.ConfirmedBy = ""
	f.rows[a.ID] = a
	return a, nil
}

// Get implements limitation.Store.
func (f *Fake) Get(_ context.Context, id string) (limitation.Assessment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.rows[id]
	if !ok {
		return limitation.Assessment{}, fmt.Errorf("get assessment %s: %w", id, limitation.ErrNotFound)
	}
	return a, nil
}

// ForMatter implements limitation.Store, ordered by id.
func (f *Fake) ForMatter(_ context.Context, matterID string) ([]limitation.Assessment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []limitation.Assessment
	for _, a := range f.rows {
		if a.MatterID == matterID {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(x, y limitation.Assessment) int {
		if len(x.ID) != len(y.ID) {
			return len(x.ID) - len(y.ID)
		}
		if x.ID < y.ID {
			return -1
		}
		return 1
	})
	return out, nil
}

// Confirm implements limitation.Store.
func (f *Fake) Confirm(_ context.Context, id, by string) (limitation.Assessment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.rows[id]
	if !ok {
		return limitation.Assessment{}, fmt.Errorf("confirm %s: %w", id, limitation.ErrNotFound)
	}
	if a.ConfirmedBy != "" {
		return limitation.Assessment{}, fmt.Errorf("confirm %s: %w", id, limitation.ErrConfirmed)
	}
	if by == "" || by == a.PreparedBy {
		return limitation.Assessment{}, fmt.Errorf("confirm %s: %w", id, limitation.ErrSelfConfirm)
	}
	a.ConfirmedBy = by
	f.rows[id] = a
	return a, nil
}
