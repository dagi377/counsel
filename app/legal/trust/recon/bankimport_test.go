package recon

import (
	"context"
	"errors"
	"testing"
	"time"
)

type lineKey struct{ account, id string }

type memLines map[lineKey]StatementLine

func (m memLines) Has(_ context.Context, account, id string) (bool, error) {
	_, ok := m[lineKey{account, id}]
	return ok, nil
}

func (m memLines) Add(_ context.Context, account string, l StatementLine) error {
	m[lineKey{account, l.ID}] = l
	return nil
}

func statement(account string, opening int64, lines ...StatementLine) Statement {
	closing := opening
	for _, l := range lines {
		closing += l.AmountCents
	}
	return Statement{AccountID: account, Period: Period{2026, time.September}, OpeningCents: opening, ClosingCents: closing, Lines: lines}
}

func line(id string, cents int64) StatementLine {
	return StatementLine{ID: id, Date: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), AmountCents: cents}
}

func TestImportLinesDedup(t *testing.T) {
	ctx := context.Background()
	store := memLines{}
	s := statement("acc1", 0, line("b1", 500000), line("b2", -100000), line("b2", -100000))
	s.ClosingCents = 300000
	cases := []struct {
		name           string
		s              Statement
		added, skipped int
	}{
		{"first import", s, 2, 1},
		{"re-import adds zero rows", s, 0, 3},
		{"same line id on another account", statement("acc2", 0, line("b1", 500000)), 1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := ImportLines(ctx, store, c.s, nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.Added != c.added || res.Skipped != c.skipped {
				t.Fatalf("got added %d skipped %d, want %d %d", res.Added, res.Skipped, c.added, c.skipped)
			}
		})
	}
	if len(store) != 3 {
		t.Fatalf("store has %d rows, want 3", len(store))
	}
}

// rule: ARC-U-032
func TestARC_U_032_StatementBalanceAndGapCheck(t *testing.T) {
	ctx := context.Background()
	prev := statement("acc1", 0, line("a1", 100000))
	bad := statement("acc1", 100000, line("c1", 12500))
	bad.ClosingCents = 100000
	cases := []struct {
		name    string
		s       Statement
		prev    *Statement
		wantErr error
		wantGap bool
	}{
		{"continues previous", statement("acc1", 100000, line("c1", 12500)), &prev, nil, false},
		{"gap after previous", statement("acc1", 90000, line("c1", 12500)), &prev, nil, true},
		{"lines do not sum to closing", bad, &prev, ErrStatementMismatch, false},
		{"line without bank id", statement("acc1", 100000, line("", 1)), nil, ErrStatementMismatch, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := memLines{}
			res, err := ImportLines(ctx, store, c.s, c.prev)
			if !errors.Is(err, c.wantErr) && !(c.wantErr == nil && err == nil) {
				t.Fatalf("got %v, want %v", err, c.wantErr)
			}
			if c.wantErr != nil && len(store) != 0 {
				t.Fatalf("refused import wrote %d rows", len(store))
			}
			if res.Gap != c.wantGap {
				t.Fatalf("gap %v, want %v", res.Gap, c.wantGap)
			}
		})
	}
}

// rule: BIL-I-009
func TestBIL_I_009_ControlEqualsClientLedgers(t *testing.T) {
	cases := []struct {
		name    string
		control int64
		ledgers []int64
		want    error
	}{
		{"balanced", 750000, []int64{500000, 250000}, nil},
		{"no ledgers, empty account", 0, nil, nil},
		{"off by one cent", 750001, []int64{500000, 250000}, ErrUnbalanced},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckJournal(c.control, c.ledgers)
			if !errors.Is(err, c.want) && !(c.want == nil && err == nil) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}
