package rates

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

// rule: BIL-C-004
func TestRepriceEffectiveDated(t *testing.T) {
	oct1 := clock.Date{Year: 2026, Month: time.October, Day: 1}
	sep30 := clock.Date{Year: 2026, Month: time.September, Day: 30}
	oct15 := clock.Date{Year: 2026, Month: time.October, Day: 15}
	base := RateRow{Source: SourceUserDefault, UserID: "u1", RateCents: 30000, EffectiveFrom: from}
	change := RateRow{Source: SourceUserDefault, UserID: "u1", RateCents: 35000, EffectiveFrom: oct1}
	old := Resolved{RateCents: 30000, Source: SourceUserDefault}
	manual := int64(50000)
	entry := func(id string, d clock.Date) Entry {
		in := input
		in.WorkDate = d
		return Entry{ID: id, Input: in, Resolved: old}
	}
	tests := []struct {
		name    string
		entry   Entry
		reprice bool
	}{
		{"dated before change is kept", entry("e1", sep30), false},
		{"dated on effective date is repriced", entry("e2", oct1), true},
		{"dated after effective date is repriced", entry("e3", oct15), true},
		{"billed entry is kept", func() Entry { e := entry("e4", oct15); e.Billed = true; return e }(), false},
		{"manual rate is kept", func() Entry {
			e := entry("e5", oct15)
			e.Input.ManualRateCents, e.Input.CanSetRates = &manual, true
			e.Resolved = Resolved{RateCents: manual, Source: SourceManual}
			return e
		}(), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := store(t, []RateRow{base})
			got, err := s.Reprice(context.Background(), change, []Entry{tc.entry})
			if err != nil {
				t.Fatalf("reprice: %v", err)
			}
			if !tc.reprice {
				if len(got) != 0 {
					t.Fatalf("got %d changed entries, want 0", len(got))
				}
				return
			}
			if len(got) != 1 || got[0].ID != tc.entry.ID {
				t.Fatalf("got %+v, want only %s", got, tc.entry.ID)
			}
			want := Resolved{RateCents: 35000, Source: SourceUserDefault}
			if got[0].Resolved != want {
				t.Fatalf("resolved = %+v, want %+v", got[0].Resolved, want)
			}
		})
	}
}

func TestRepriceUnchangedRateNotReturned(t *testing.T) {
	oct1 := clock.Date{Year: 2026, Month: time.October, Day: 1}
	s := store(t, []RateRow{{Source: SourceMatterUser, MatterID: "m1", UserID: "u1", RateCents: 30000, EffectiveFrom: from}})
	e := Entry{ID: "e1", Input: input, Resolved: Resolved{RateCents: 30000, Source: SourceMatterUser}}
	e.Input.WorkDate = oct1
	// A lower-priority tier changes; the matter rate still wins, so nothing changes.
	got, err := s.Reprice(context.Background(), RateRow{Source: SourceUserDefault, UserID: "u1", RateCents: 99000, EffectiveFrom: oct1}, []Entry{e})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want none", got, err)
	}
}

func TestRepriceBadRow(t *testing.T) {
	s := store(t, nil)
	_, err := s.Reprice(context.Background(), RateRow{Source: SourceManual, RateCents: 1}, nil)
	if !errors.Is(err, ErrBadRow) {
		t.Fatalf("err = %v, want ErrBadRow", err)
	}
}
