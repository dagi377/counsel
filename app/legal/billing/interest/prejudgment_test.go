package interest

import (
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/kernel/money"
)

// rule: BIL-L-010 (verify)
func TestBIL_L_010_PrejudgmentRateByQuarter(t *testing.T) {
	loc := toronto(t)
	tests := []struct {
		name string
		d    time.Time
		want money.PPM
		err  error
	}{
		{"2020 Q1 first day", time.Date(2020, 1, 1, 0, 0, 0, 0, loc), 20000, nil},
		{"2022 Q3", time.Date(2022, 8, 15, 0, 0, 0, 0, loc), 18000, nil},
		{"2023 Q4 last day", time.Date(2023, 12, 31, 0, 0, 0, 0, loc), 53000, nil},
		{"2025 Q2", time.Date(2025, 4, 1, 0, 0, 0, 0, loc), 33000, nil},
		{"2026 Q4", time.Date(2026, 10, 11, 0, 0, 0, 0, loc), 25000, nil},
		{"before the table", time.Date(2019, 12, 31, 0, 0, 0, 0, loc), 0, ErrNoPrejudgmentRate},
		{"not yet published", time.Date(2027, 1, 1, 0, 0, 0, 0, loc), 0, ErrNoPrejudgmentRate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PrejudgmentRate(tt.d)
			if !errors.Is(err, tt.err) || got != tt.want {
				t.Fatalf("PrejudgmentRate = %d, %v; want %d, %v", got, err, tt.want, tt.err)
			}
		})
	}
}

// Every quarter from the first seeded one to the last is present: a missing quarter would
// silently fail closed for bills that should have a benchmark.
func TestPrejudgmentRatesContiguous(t *testing.T) {
	for y := 2020; y <= 2026; y++ {
		for q := 1; q <= 4; q++ {
			if r, ok := prejudgmentRates[quarter{y, q}]; !ok || r <= 0 {
				t.Errorf("%d Q%d missing or zero", y, q)
			}
		}
	}
}

// With no agreed rate, interest runs at the CJA prejudgment rate for the quarter of delivery
// only while interest.cja_default_rate is on; an agreed rate always wins.
func TestBIL_L_010_EffectiveRateCJADefault(t *testing.T) {
	// 2026-01-01 03:00 UTC is still 2025-12-31 in Toronto: the delivery quarter is 2025 Q4.
	newYearsEve := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		flagOn  bool
		bill    Bill
		want    money.PPM
		wantErr error
	}{
		{"agreed rate, flag off", false, Bill{ID: "b1", RatePPM: 60000, DeliveredAt: newYearsEve}, 60000, nil},
		{"agreed rate, flag on", true, Bill{ID: "b1", RatePPM: 60000, DeliveredAt: newYearsEve}, 60000, nil},
		{"no rate, flag off", false, Bill{ID: "b2", DeliveredAt: newYearsEve}, 0, ErrNoAgreedRate},
		{"no rate, flag on, Toronto quarter", true, Bill{ID: "b2", DeliveredAt: newYearsEve}, 30000, nil},
		{"no rate, flag on, 2026 Q1", true, Bill{ID: "b3", DeliveredAt: time.Date(2026, 2, 10, 15, 0, 0, 0, time.UTC)}, 25000, nil},
		{"no rate, flag on, not delivered", true, Bill{ID: "b4"}, 0, ErrNotDelivered},
		{"no rate, flag on, unseeded quarter", true, Bill{ID: "b5", DeliveredAt: time.Date(2030, 5, 1, 15, 0, 0, 0, time.UTC)}, 0, ErrNoPrejudgmentRate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := legalflagstest.Context(t, FlagCJADefaultRate, tt.flagOn)
			got, err := EffectiveRate(ctx, tt.bill)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("rate = %d, want %d", got, tt.want)
			}
		})
	}
}
