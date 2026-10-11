package timekeeping

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

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

// rateTx answers entryRateSQL with rate when the table argument is table ("" is the default
// table), and records the table it was asked for.
type rateTx struct {
	pgx.Tx
	asked string
	rates map[string]int64
}

func (t *rateTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	t.asked = args[0].(string)
	return rateRow{rate: t.rates[t.asked]}
}

type rateRow struct{ rate int64 }

func (r rateRow) Scan(dest ...any) error {
	if r.rate == 0 {
		return pgx.ErrNoRows
	}
	*dest[0].(*int64) = r.rate
	return nil
}

// TestEntryRateTable: the matter's own rate table prices the entry; the default table is used
// only when the matter has none.
func TestEntryRateTable(t *testing.T) {
	tests := []struct {
		name, table, source string
		rate                int64
	}{
		{"matter table", "rt-matter", "matter_user", 45_000},
		{"no matter table: default", "", "user_default", 30_000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := &rateTx{rates: map[string]int64{"rt-matter": 45_000, "": 30_000}}
			rate, source, err := entryRate(context.Background(), tx, tc.table, "u1", "2026-09-01", "")
			if err != nil || rate != tc.rate || source != tc.source || tx.asked != tc.table {
				t.Fatalf("rate = %d %s %v (asked %q), want %d %s", rate, source, err, tx.asked, tc.rate, tc.source)
			}
		})
	}
}
