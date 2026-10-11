package interest

import (
	"errors"
	"testing"
	"time"

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
