//go:build integration

package ledger

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// rule: BIL-I-025
func TestBIL_I_025_PeriodMustBeWholeMonths(t *testing.T) {
	d := func(y int, m time.Month, day int) Date { return Date{Year: y, Month: m, Day: day} }
	tests := []struct {
		name string
		p    Period
		want int
		err  error
	}{
		{"one month", Period{d(2026, time.February, 1), d(2026, time.February, 28)}, 1, nil},
		{"quarter across year", Period{d(2025, time.December, 1), d(2026, time.February, 28)}, 3, nil},
		{"leap february", Period{d(2028, time.February, 1), d(2028, time.February, 29)}, 1, nil},
		{"mid-month start", Period{d(2026, time.March, 2), d(2026, time.March, 31)}, 0, ErrInvalidEntry},
		{"mid-month end", Period{d(2026, time.March, 1), d(2026, time.March, 30)}, 0, ErrInvalidEntry},
		{"reversed", Period{d(2026, time.April, 1), d(2026, time.March, 31)}, 0, ErrInvalidEntry},
		{"impossible date", Period{d(2026, time.February, 1), d(2026, time.February, 30)}, 0, ErrInvalidEntry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := months(tt.p)
			if !errors.Is(err, tt.err) || len(got) != tt.want {
				t.Fatalf("months = %v, %v; want %d months, %v", got, err, tt.want, tt.err)
			}
		})
	}
}

// rule: BIL-I-025
func TestBIL_I_025_ClosedPeriodRefusesPostingCorrectionInCurrent(t *testing.T) {
	conn, user := freshDB(t)
	ctx := context.Background()
	g := &PG{PostedBy: func(context.Context) (string, error) { return user, nil }}
	march := Period{From: Date{Year: 2026, Month: time.March, Day: 1}, To: Date{Year: 2026, Month: time.March, Day: 31}}
	entry := func(n int, d Date) Entry {
		return Entry{Book: "main", Date: d, SourceKind: "manual", SourceID: uuidish(n), Memo: "t",
			Lines: []Line{line("1000", Debit, 500), line("4000", Credit, 500)}}
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Post(ctx, tx, entry(1, Date{Year: 2026, Month: time.March, Day: 15})); err != nil {
		t.Fatalf("post before close: %v", err)
	}
	if err := g.ClosePeriod(ctx, tx, "main", march); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := g.ClosePeriod(ctx, tx, "main", march); err != nil {
		t.Fatalf("close again: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	tests := []struct {
		name    string
		date    Date
		refused bool
	}{
		{"first day of closed month", Date{Year: 2026, Month: time.March, Day: 1}, true},
		{"last day of closed month", Date{Year: 2026, Month: time.March, Day: 31}, true},
		{"correction in current period", Date{Year: 2026, Month: time.April, Day: 2}, false},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := g.CheckOpen(ctx, tx, "main", tt.date); errors.Is(err, ErrPeriodClosed) != tt.refused {
				t.Fatalf("CheckOpen = %v, refused want %v", err, tt.refused)
			}
			_, err = g.Post(ctx, tx, entry(10+i, tt.date))
			if tt.refused {
				if !errors.Is(err, ErrPeriodClosed) {
					t.Fatalf("post = %v, want ErrPeriodClosed", err)
				}
				// The trigger still refuses a direct insert that skips CheckOpen.
				_, err = tx.Exec(ctx, sqlInsertEntry, monthStart(tt.date), "t", "manual", uuidish(50+i), user, "main")
				if err == nil || !strings.Contains(err.Error(), "DM-062") {
					t.Fatalf("direct insert = %v, want DM-062", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatalf("commit: %v", err)
			}
		})
	}
}
