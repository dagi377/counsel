package rates

import (
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

// rule: BIL-C-010
func TestBIL_C_010_TimeRounding(t *testing.T) {
	tests := []struct {
		name string
		raw  int
		r    Rounding
		want int
		err  error
	}{
		{"7 min up by 6", 7, Rounding{IncrementMinutes: 6, Mode: ModeUp}, 12, nil},
		{"7 min nearest by 6", 7, Rounding{IncrementMinutes: 6, Mode: ModeNearest}, 6, nil},
		{"BIL-X-001 23 up", 23, Rounding{IncrementMinutes: 6, Mode: ModeUp}, 24, nil},
		{"BIL-X-001 20 nearest", 20, Rounding{IncrementMinutes: 6, Mode: ModeNearest}, 18, nil},
		{"BIL-X-001 21 nearest is half up", 21, Rounding{IncrementMinutes: 6, Mode: ModeNearest}, 24, nil},
		{"exact multiple up", 12, Rounding{IncrementMinutes: 6, Mode: ModeUp}, 12, nil},
		{"zero stays zero", 0, Rounding{IncrementMinutes: 6, Mode: ModeUp}, 0, nil},
		{"increment 1", 7, Rounding{IncrementMinutes: 1, Mode: ModeUp}, 7, nil},
		{"increment 15 up", 16, Rounding{IncrementMinutes: 15, Mode: ModeUp}, 30, nil},
		{"increment 15 nearest below half", 22, Rounding{IncrementMinutes: 15, Mode: ModeNearest}, 15, nil},
		{"increment 15 nearest above half", 23, Rounding{IncrementMinutes: 15, Mode: ModeNearest}, 30, nil},
		{"increment 10 refused", 7, Rounding{IncrementMinutes: 10, Mode: ModeUp}, 0, ErrBadRounding},
		{"unknown mode refused", 7, Rounding{IncrementMinutes: 6, Mode: "down"}, 0, ErrBadRounding},
		{"negative raw refused", -1, Rounding{IncrementMinutes: 6, Mode: ModeUp}, 0, ErrBadRounding},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := roundMinutes(tt.raw, tt.r)
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if got != tt.want {
				t.Fatalf("roundMinutes(%d, %+v) = %d, want %d", tt.raw, tt.r, got, tt.want)
			}
		})
	}
}

// rule: BIL-C-010
func TestBIL_C_010_RoundedMinutesPriceTheLine(t *testing.T) {
	billed, err := roundMinutes(7, Rounding{IncrementMinutes: 6, Mode: ModeUp})
	if err != nil {
		t.Fatal(err)
	}
	// rule: BIL-C-011
	amt, err := moneyfake.New().Rhu(int64(billed)*45000, 60)
	if err != nil || amt != 9000 {
		t.Fatalf("rhu(%d*45000, 60) = %d, %v; want 9000", billed, amt, err)
	}
}
