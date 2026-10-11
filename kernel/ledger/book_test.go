package ledger

import (
	"context"
	"errors"
	"testing"
	"time"
)

// rule: BIL-I-025
func TestBIL_I_025_PostAndCloseArePerBook(t *testing.T) {
	d := Date{Year: 2026, Month: time.March, Day: 15}
	tests := []struct {
		name string
		book string
	}{
		{"first book", "main"},
		{"second book", "side"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeTx{}
			g := &PG{PostedBy: poster}
			e := Entry{Book: tt.book, Date: d, SourceKind: "manual", SourceID: "s1",
				Lines: []Line{line("1000", Debit, 500), line("4000", Credit, 500)}}
			if _, err := g.Post(context.Background(), tx, e); err != nil {
				t.Fatalf("Post: %v", err)
			}
			if got := tx.rowArgs[sqlPeriodClosed]; len(got) != 2 || got[0] != tt.book {
				t.Fatalf("period check args = %v, want book %q first", got, tt.book)
			}
			if got := tx.rowArgs[sqlInsertEntry]; len(got) != 6 || got[5] != tt.book {
				t.Fatalf("insert entry args = %v, want book %q last", got, tt.book)
			}

			ctx := context.Background()
			tx = &fakeTx{}
			march := Period{From: Date{Year: 2026, Month: time.March, Day: 1}, To: Date{Year: 2026, Month: time.March, Day: 31}}
			if err := g.ClosePeriod(ctx, tx, tt.book, march); err != nil {
				t.Fatalf("ClosePeriod: %v", err)
			}
			if len(tx.execs) != 1 || tx.execs[0].sql != sqlClosePeriod || tx.execs[0].args[0] != tt.book {
				t.Fatalf("close execs = %+v, want one per-book close of %q", tx.execs, tt.book)
			}
		})
	}
}

// rule: BIL-I-025
func TestBIL_I_025_CheckOpenNeedsBook(t *testing.T) {
	g := &PG{PostedBy: poster}
	d := Date{Year: 2026, Month: time.March, Day: 15}
	tests := []struct {
		name   string
		book   string
		closed bool
		want   error
	}{
		{"no book", "", false, ErrInvalidEntry},
		{"open month", "main", false, nil},
		{"closed month", "main", true, ErrPeriodClosed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := g.CheckOpen(context.Background(), &fakeTx{closed: tt.closed}, tt.book, d)
			if !errors.Is(err, tt.want) {
				t.Fatalf("CheckOpen = %v, want %v", err, tt.want)
			}
		})
	}
}
