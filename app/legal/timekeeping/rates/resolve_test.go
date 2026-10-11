package rates

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

var (
	day   = clock.Date{Year: 2026, Month: time.September, Day: 15}
	input = Input{MatterID: "m1", ClientID: "c1", UserID: "u1", RoleGroup: "partner", ActivityID: "a1", WorkDate: day}
	from  = clock.Date{Year: 2026, Month: time.January, Day: 1}
)

// tierRows has one row per tier 2..8, each with a distinct rate.
func tierRows() []RateRow {
	return []RateRow{
		{Source: SourceFlatActivity, ActivityID: "a1", RateCents: 2000, EffectiveFrom: from},
		{Source: SourceMatterUser, MatterID: "m1", UserID: "u1", RateCents: 3000, EffectiveFrom: from},
		{Source: SourceMatterRole, MatterID: "m1", RoleGroup: "partner", RateCents: 4000, EffectiveFrom: from},
		{Source: SourceClientUser, ClientID: "c1", UserID: "u1", RateCents: 5000, EffectiveFrom: from},
		{Source: SourceClientRole, ClientID: "c1", RoleGroup: "partner", RateCents: 6000, EffectiveFrom: from},
		{Source: SourceActivity, ActivityID: "a1", RateCents: 7000, EffectiveFrom: from},
		{Source: SourceUserDefault, UserID: "u1", RateCents: 8000, EffectiveFrom: from},
	}
}

func store(t *testing.T, rows []RateRow) *Store {
	t.Helper()
	s := NewStore(moneyfake.New())
	for _, r := range rows {
		if err := s.Put(context.Background(), r); err != nil {
			t.Fatalf("put %s: %v", r.Source, err)
		}
	}
	return s
}

// rule: BIL-C-003
func TestBIL_C_003_ResolutionChainFirstHitWins(t *testing.T) {
	manual := int64(1000)
	all := tierRows()
	tests := []struct {
		name   string
		rows   []RateRow
		manual *int64
		can    bool
		want   Resolved
		err    error
	}{
		{"tier 1 manual", all, &manual, true, Resolved{1000, SourceManual}, nil},
		{"tier 2 flat activity", all, nil, false, Resolved{2000, SourceFlatActivity}, nil},
		{"tier 3 matter user", all[1:], nil, false, Resolved{3000, SourceMatterUser}, nil},
		{"tier 4 matter role", all[2:], nil, false, Resolved{4000, SourceMatterRole}, nil},
		{"tier 5 client user", all[3:], nil, false, Resolved{5000, SourceClientUser}, nil},
		{"tier 6 client role", all[4:], nil, false, Resolved{6000, SourceClientRole}, nil},
		{"tier 7 activity", all[5:], nil, false, Resolved{7000, SourceActivity}, nil},
		{"tier 8 user default", all[6:], nil, false, Resolved{8000, SourceUserDefault}, nil},
		{"manual without time.rates is forbidden", all, &manual, false, Resolved{}, ErrManualRateForbidden},
		{"no tier hits", nil, nil, false, Resolved{}, ErrNoRate},
		{"BIL-X-002 matter role beats client user and default", []RateRow{
			{Source: SourceUserDefault, UserID: "u1", RateCents: 20000, EffectiveFrom: from},
			{Source: SourceClientUser, ClientID: "c1", UserID: "u1", RateCents: 18200, EffectiveFrom: from},
			{Source: SourceMatterRole, MatterID: "m1", RoleGroup: "partner", RateCents: 17500, EffectiveFrom: from},
		}, nil, false, Resolved{17500, SourceMatterRole}, nil},
		{"other matter does not match", []RateRow{
			{Source: SourceMatterUser, MatterID: "m2", UserID: "u1", RateCents: 9900, EffectiveFrom: from},
			{Source: SourceUserDefault, UserID: "u1", RateCents: 8000, EffectiveFrom: from},
		}, nil, false, Resolved{8000, SourceUserDefault}, nil},
		{"row effective after work date is skipped", []RateRow{
			{Source: SourceMatterUser, MatterID: "m1", UserID: "u1", RateCents: 9900, EffectiveFrom: clock.Date{Year: 2026, Month: time.October, Day: 1}},
			{Source: SourceUserDefault, UserID: "u1", RateCents: 8000, EffectiveFrom: from},
		}, nil, false, Resolved{8000, SourceUserDefault}, nil},
		{"latest effective row within a tier wins", []RateRow{
			{Source: SourceUserDefault, UserID: "u1", RateCents: 40000, EffectiveFrom: from},
			{Source: SourceUserDefault, UserID: "u1", RateCents: 42500, EffectiveFrom: clock.Date{Year: 2026, Month: time.September, Day: 1}},
		}, nil, false, Resolved{42500, SourceUserDefault}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := input
			in.ManualRateCents, in.CanSetRates = tt.manual, tt.can
			got, err := store(t, tt.rows).Resolve(context.Background(), in)
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPutRejectsBadRows(t *testing.T) {
	tests := []struct {
		name string
		row  RateRow
	}{
		{"manual source", RateRow{Source: SourceManual, UserID: "u1", RateCents: 1}},
		{"missing key", RateRow{Source: SourceMatterUser, MatterID: "m1", RateCents: 1}},
		{"negative rate", RateRow{Source: SourceUserDefault, UserID: "u1", RateCents: -1}},
		{"unknown source", RateRow{Source: "other", UserID: "u1", RateCents: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := NewStore(moneyfake.New()).Put(context.Background(), tt.row); !errors.Is(err, ErrBadRow) {
				t.Fatalf("err = %v, want ErrBadRow", err)
			}
		})
	}
}

// rule: BIL-C-011
func TestBIL_C_011_TimeLineAmount(t *testing.T) {
	tests := []struct {
		name    string
		minutes int
		rate    int64
		want    int64
		err     bool
	}{
		{"24 min at 42500", 24, 42500, 17000, false},
		{"12 min at 45000", 12, 45000, 9000, false},
		{"half cent rounds up", 1, 30, 1, false},
		{"under half cent rounds down", 1, 29, 0, false},
		{"zero minutes", 0, 42500, 0, false},
		{"negative minutes", -6, 42500, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewStore(moneyfake.New()).Amount(tt.minutes, tt.rate)
			if (err != nil) != tt.err || got != tt.want {
				t.Fatalf("Amount(%d, %d) = %d, %v; want %d, err %v", tt.minutes, tt.rate, got, err, tt.want, tt.err)
			}
		})
	}
}

// rule: BIL-C-012
func TestBIL_C_012_MinimumEntry(t *testing.T) {
	tests := []struct {
		name                 string
		raw, billed, minimum int
		want                 int
	}{
		{"minimum 6 raw 2", 2, 2, 6, 6},
		{"off when zero", 2, 2, 0, 2},
		{"no time recorded", 0, 0, 6, 0},
		{"above minimum kept", 12, 12, 6, 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyMinimum(tt.raw, tt.billed, tt.minimum); got != tt.want {
				t.Fatalf("applyMinimum(%d, %d, %d) = %d, want %d", tt.raw, tt.billed, tt.minimum, got, tt.want)
			}
		})
	}
}
