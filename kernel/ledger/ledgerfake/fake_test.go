package ledgerfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/ledger"
)

func d(m time.Month, day int) ledger.Date { return ledger.Date{Year: 2026, Month: m, Day: day} }

func entry(src string, date ledger.Date, amt ledger.Cents) ledger.Entry {
	return ledger.Entry{Book: "gl", Date: date, SourceKind: "invoice", SourceID: src, Lines: []ledger.Line{
		{Account: "1000", Side: ledger.Debit, Amount: amt},
		{Account: "4000", Side: ledger.Credit, Amount: amt},
	}}
}

func TestPost(t *testing.T) {
	unbalanced := entry("u", d(3, 1), 100)
	unbalanced.Lines[1].Amount = 99
	badSide := entry("s", d(3, 1), 100)
	badSide.Lines[0].Side = "left"
	tests := []struct {
		name string
		e    ledger.Entry
		err  error
	}{
		{name: "balanced", e: entry("a", d(3, 1), 1000)},
		{name: "unbalanced", e: unbalanced, err: ledger.ErrUnbalanced},
		{name: "zero amount", e: entry("z", d(3, 1), 0), err: ledger.ErrInvalidEntry},
		{name: "no source", e: entry("", d(3, 1), 5), err: ledger.ErrInvalidEntry},
		{name: "bad side", e: badSide, err: ledger.ErrInvalidEntry},
		{name: "unknown account", e: ledger.Entry{Book: "gl", Date: d(3, 1), SourceKind: "k", SourceID: "1", Lines: []ledger.Line{
			{Account: "9999", Side: ledger.Debit, Amount: 1}, {Account: "4000", Side: ledger.Credit, Amount: 1}}}, err: ledger.ErrUnknownAccount},
		{name: "closed period", e: entry("c", d(1, 15), 5), err: ledger.ErrPeriodClosed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New(d(3, 31), "1000", "4000")
			if err := f.ClosePeriod(ctx, nil, "gl", ledger.Period{From: d(1, 1), To: d(1, 31)}); err != nil {
				t.Fatal(err)
			}
			id, err := f.Post(ctx, nil, tt.e)
			if !errors.Is(err, tt.err) {
				t.Fatalf("Post err = %v, want %v", err, tt.err)
			}
			if err != nil {
				return
			}
			again, err := f.Post(ctx, nil, tt.e)
			if err != nil || again != id {
				t.Fatalf("second Post = %q, %v; want %q (idempotent per source)", again, err, id)
			}
		})
	}
}

func TestReverseAndBalance(t *testing.T) {
	tests := []struct {
		name    string
		reverse bool
		asOf    ledger.Date
		want    ledger.Cents
	}{
		{name: "posted", asOf: d(3, 31), want: 1000},
		{name: "before posting date", asOf: d(2, 28), want: 0},
		{name: "reversed nets to zero", reverse: true, asOf: d(3, 31), want: 0},
		{name: "reversal dated later", reverse: true, asOf: d(3, 10), want: 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New(d(3, 31))
			id, err := f.Post(ctx, nil, entry("a", d(3, 1), 1000))
			if err != nil {
				t.Fatal(err)
			}
			if tt.reverse {
				rid, err := f.Reverse(ctx, nil, id, "entered twice")
				if err != nil {
					t.Fatal(err)
				}
				orig, _ := f.Entry(id)
				rev, _ := f.Entry(rid)
				for i := range orig.Lines {
					if orig.Lines[i].Account != rev.Lines[i].Account || orig.Lines[i].Amount != rev.Lines[i].Amount || orig.Lines[i].Side == rev.Lines[i].Side {
						t.Fatalf("reversal line %d = %+v, original %+v", i, rev.Lines[i], orig.Lines[i])
					}
				}
				if _, err := f.Reverse(ctx, nil, id, "again"); !errors.Is(err, ledger.ErrAlreadyReversed) {
					t.Fatalf("second Reverse err = %v", err)
				}
			}
			got, err := f.Balance(ctx, "1000", tt.asOf)
			if err != nil || got != tt.want {
				t.Fatalf("Balance = %d, %v; want %d", got, err, tt.want)
			}
		})
	}
	if _, err := New(d(1, 1)).Reverse(context.Background(), nil, "nope", "x"); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("Reverse missing err = %v", err)
	}
}
