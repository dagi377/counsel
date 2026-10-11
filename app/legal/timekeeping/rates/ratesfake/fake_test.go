package ratesfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/timekeeping/rates"
)

var (
	sep1 = day(2026, 9, 1)
	aug1 = day(2026, 8, 1)
)

func day(y, m, d int) time.Time { return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC) }

func cents(v int64) *int64 { return &v }

func seed(t *testing.T) *Fake {
	t.Helper()
	f := New()
	rows := []rates.RateRow{
		{Source: rates.SourceUserDefault, UserID: "u1", RateCents: 20000, EffectiveFrom: aug1},
		{Source: rates.SourceClientUser, ClientID: "c1", UserID: "u1", RateCents: 18200, EffectiveFrom: aug1},
		{Source: rates.SourceMatterRole, MatterID: "m1", RoleGroup: "assoc", RateCents: 17500, EffectiveFrom: aug1},
		{Source: rates.SourceMatterUser, MatterID: "m2", UserID: "u1", RateCents: 25000, EffectiveFrom: aug1},
	}
	for _, r := range rows {
		if err := f.Put(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func TestFakeResolve(t *testing.T) {
	base := rates.Input{MatterID: "m1", ClientID: "c1", UserID: "u1", RoleGroup: "assoc", WorkDate: sep1}
	tests := []struct {
		name    string
		mut     func(in *rates.Input)
		want    rates.Resolved
		wantErr error
	}{
		{"matter role beats client user and default", func(*rates.Input) {}, rates.Resolved{RateCents: 17500, Source: rates.SourceMatterRole}, nil},
		{"client user when no matter rate", func(in *rates.Input) { in.MatterID = "m9" }, rates.Resolved{RateCents: 18200, Source: rates.SourceClientUser}, nil},
		{"user default last", func(in *rates.Input) { in.MatterID, in.ClientID = "m9", "c9" }, rates.Resolved{RateCents: 20000, Source: rates.SourceUserDefault}, nil},
		{"manual with right", func(in *rates.Input) { in.ManualRateCents, in.CanSetRates = cents(30000), true }, rates.Resolved{RateCents: 30000, Source: rates.SourceManual}, nil},
		{"manual without right", func(in *rates.Input) { in.ManualRateCents = cents(30000) }, rates.Resolved{}, rates.ErrManualRateForbidden},
		{"before effective date", func(in *rates.Input) { in.WorkDate = day(2026, 7, 31) }, rates.Resolved{}, rates.ErrNoRate},
	}
	f := seed(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base
			tt.mut(&in)
			got, err := f.Resolve(context.Background(), in)
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Fatalf("Resolve = %+v, %v; want %+v, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestFakeBilledMinutesAndAmount(t *testing.T) {
	tests := []struct {
		name    string
		raw     int
		r       rates.Rounding
		rate    int64
		minutes int
		amount  int64
		wantErr error
	}{
		{"23 up", 23, rates.Rounding{IncrementMinutes: 6, Mode: rates.ModeUp}, 42500, 24, 17000, nil},
		{"20 nearest", 20, rates.Rounding{IncrementMinutes: 6, Mode: rates.ModeNearest}, 0, 18, 0, nil},
		{"21 nearest half up", 21, rates.Rounding{IncrementMinutes: 6, Mode: rates.ModeNearest}, 0, 24, 0, nil},
		{"minimum entry", 2, rates.Rounding{IncrementMinutes: 1, Mode: rates.ModeUp, MinimumEntry: 6}, 0, 6, 0, nil},
		{"zero stays zero", 0, rates.Rounding{IncrementMinutes: 1, Mode: rates.ModeUp, MinimumEntry: 6}, 0, 0, 0, nil},
		{"bad increment", 5, rates.Rounding{IncrementMinutes: 10, Mode: rates.ModeUp}, 0, 0, 0, rates.ErrBadRounding},
	}
	f := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.BilledMinutes(tt.raw, tt.r)
			if !errors.Is(err, tt.wantErr) || got != tt.minutes {
				t.Fatalf("BilledMinutes = %d, %v; want %d, %v", got, err, tt.minutes, tt.wantErr)
			}
			if amt, err := f.Amount(got, tt.rate); err != nil || amt != tt.amount {
				t.Fatalf("Amount = %d, %v; want %d", amt, err, tt.amount)
			}
		})
	}
}

func TestFakeRepriceAndMove(t *testing.T) {
	ctx := context.Background()
	in := rates.Input{UserID: "u1", MatterID: "m9", ClientID: "c9", WorkDate: sep1}
	old := rates.Input{UserID: "u1", MatterID: "m9", ClientID: "c9", WorkDate: day(2026, 8, 31)}
	def := rates.Resolved{RateCents: 20000, Source: rates.SourceUserDefault}
	tests := []struct {
		name    string
		entries []rates.Entry
		want    []string
	}{
		{"on or after effective date", []rates.Entry{{ID: "a", Input: in, Resolved: def}}, []string{"a"}},
		{"before effective date", []rates.Entry{{ID: "b", Input: old, Resolved: def}}, nil},
		{"billed untouched", []rates.Entry{{ID: "c", Input: in, Resolved: def, Billed: true}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := seed(t)
			row := rates.RateRow{Source: rates.SourceUserDefault, UserID: "u1", RateCents: 21000, EffectiveFrom: sep1}
			got, err := f.Reprice(ctx, row, tt.entries)
			if err != nil || len(got) != len(tt.want) {
				t.Fatalf("Reprice = %+v, %v; want ids %v", got, err, tt.want)
			}
			for i := range got {
				if got[i].ID != tt.want[i] || got[i].Resolved.RateCents != 21000 {
					t.Fatalf("Reprice[%d] = %+v", i, got[i])
				}
			}
		})
	}

	moves := []struct {
		name    string
		e       rates.Entry
		want    rates.Resolved
		recheck bool
	}{
		{"manual kept and flagged", rates.Entry{ID: "m", Input: in, Resolved: rates.Resolved{RateCents: 30000, Source: rates.SourceManual}}, rates.Resolved{RateCents: 30000, Source: rates.SourceManual}, true},
		{"resolved re-resolves", rates.Entry{ID: "r", Input: in, Resolved: def}, rates.Resolved{RateCents: 25000, Source: rates.SourceMatterUser}, false},
	}
	for _, tt := range moves {
		t.Run(tt.name, func(t *testing.T) {
			got, err := seed(t).Move(ctx, tt.e, "m2", "c9")
			if err != nil || got.Resolved != tt.want || got.RateRecheck != tt.recheck || got.Input.MatterID != "m2" {
				t.Fatalf("Move = %+v, %v", got, err)
			}
		})
	}
}
