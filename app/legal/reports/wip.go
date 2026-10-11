package reports

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/dagi377/counsel/app/legal/timekeeping/rates"
	"github.com/dagi377/counsel/kernel/clock"
)

// WIPEntry is one approved, unbilled time entry with its billed minutes and rate inputs.
type WIPEntry struct {
	MatterID      string
	BilledMinutes int
	Input         rates.Input
}

// WIPSource lists approved time entries that are not on any bill at the end of day asOf.
type WIPSource interface {
	ApprovedUnbilled(ctx context.Context, asOf time.Time) ([]WIPEntry, error)
}

// WIPPricer resolves an entry's rate and prices its billed minutes.
type WIPPricer interface {
	rates.Resolver
	// Amount is rhu(billedMinutes * rateCents, 60) (BIL-C-011).
	Amount(billedMinutes int, rateCents int64) (int64, error)
}

// WIPReporter builds work-in-progress reports.
type WIPReporter struct {
	Source WIPSource
	Rates  WIPPricer
}

// WIP returns approved unbilled time at asOf at resolved rates, per matter, ordered by
// MatterID. Entries worked after asOf are left out. A flat activity rate is the line amount.
func (r WIPReporter) WIP(ctx context.Context, asOf time.Time) (WIP, error) {
	entries, err := r.Source.ApprovedUnbilled(ctx, asOf)
	if err != nil {
		return WIP{}, fmt.Errorf("wip: entries: %w", err)
	}
	day := clock.DateOf(asOf, asOf.Location())
	by := map[string]int64{}
	for _, e := range entries {
		if day.Before(e.Input.WorkDate) {
			continue
		}
		res, err := r.Rates.Resolve(ctx, e.Input)
		if err != nil {
			return WIP{}, fmt.Errorf("wip: resolve rate: %w", err)
		}
		amount := res.RateCents
		if res.Source != rates.SourceFlatActivity {
			amount, err = r.Rates.Amount(e.BilledMinutes, res.RateCents)
			if err != nil {
				return WIP{}, fmt.Errorf("wip: price entry: %w", err)
			}
		}
		by[e.MatterID] += amount
	}
	w := WIP{AsOf: asOf}
	for m, c := range by {
		w.Lines = append(w.Lines, WIPLine{MatterID: m, Cents: c})
		w.TotalCents += c
	}
	sort.Slice(w.Lines, func(i, j int) bool { return w.Lines[i].MatterID < w.Lines[j].MatterID })
	return w, nil
}
