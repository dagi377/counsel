package timekeeping

import "testing"

// TestEntryAmount prices minutes at an hourly rate in integer cents, half up.
func TestEntryAmount(t *testing.T) {
	tests := []struct {
		name     string
		billable bool
		minutes  int
		rate     int64
		want     int64
	}{
		{"one hour", true, 60, 30_000, 30_000},
		{"six minutes", true, 6, 30_000, 3_000},
		{"half cent rounds up", true, 1, 30, 1},
		{"under half a cent rounds down", true, 1, 29, 0},
		{"not billable", false, 60, 30_000, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := entryAmount(tc.billable, tc.minutes, tc.rate); got != tc.want {
				t.Fatalf("amount = %d, want %d", got, tc.want)
			}
		})
	}
}
