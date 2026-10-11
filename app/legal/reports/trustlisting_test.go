package reports

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
)

type trustSource struct {
	lines   map[string][]TrustLine
	ledger  map[string]int64
	gotDay  time.Time
	gotCode string
}

func (s *trustSource) ClientBalances(_ context.Context, account string, asOf time.Time) ([]TrustLine, error) {
	s.gotDay = asOf
	l, ok := s.lines[account]
	if !ok {
		return nil, fmt.Errorf("account %s: %w", account, ErrNotFound)
	}
	return l, nil
}

func (s *trustSource) AccountBalance(_ context.Context, account, code string, _ time.Time) (int64, error) {
	s.gotCode = code
	return s.ledger[account], nil
}

func TestTrustListingTotalsEqualTR2500PerAccount(t *testing.T) {
	src := &trustSource{
		lines: map[string][]TrustLine{
			"ta-1": {
				{ClientID: "c2", MatterID: "m3", BalanceCents: 5_000},
				{ClientID: "c1", MatterID: "m2", BalanceCents: 2_500},
				{ClientID: "c1", MatterID: "m1", BalanceCents: 10_000},
			},
			"ta-2": {{ClientID: "c3", MatterID: "m4", BalanceCents: 7_000}},
			"ta-3": {{ClientID: "c4", MatterID: "m5", BalanceCents: 1_000}},
			"ta-4": {},
		},
		ledger: map[string]int64{"ta-1": 17_500, "ta-2": 7_000, "ta-3": 999, "ta-4": 0},
	}
	tor, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	mid := time.Date(2026, time.February, 14, 0, 0, 0, 0, tor)
	tests := []struct {
		name    string
		flagOn  bool
		account string
		total   int64
		first   string
		wantErr error
	}{
		{name: "balances across matters", flagOn: true, account: "ta-1", total: 17_500, first: "m1"},
		{name: "second account kept separate", flagOn: true, account: "ta-2", total: 7_000, first: "m4"},
		{name: "empty account", flagOn: true, account: "ta-4", total: 0},
		{name: "out of balance", flagOn: true, account: "ta-3", total: 1_000, first: "m5", wantErr: ErrOutOfBalance},
		{name: "unknown account", flagOn: true, account: "nope", wantErr: ErrNotFound},
		{name: "missing account id", flagOn: true, account: "", wantErr: ErrNotFound},
		{name: "flag off", flagOn: false, account: "ta-1", wantErr: ErrDisabled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := TrustLister{Source: src}
			got, err := l.TrustListing(legalflagstest.Context(t, FlagBylaw9Verified, tc.flagOn), tc.account, mid)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil && !errors.Is(tc.wantErr, ErrOutOfBalance) {
				return
			}
			if got.TotalCents != tc.total {
				t.Errorf("total = %d, want %d", got.TotalCents, tc.total)
			}
			if tc.wantErr == nil && got.TotalCents != got.LedgerCents {
				t.Errorf("total %d != ledger %d", got.TotalCents, got.LedgerCents)
			}
			if src.gotCode != books.AcctClientTrustLedger {
				t.Errorf("ledger code = %q, want %q", src.gotCode, books.AcctClientTrustLedger)
			}
			if want := "2026-02-28"; got.MonthEnd.Format(time.DateOnly) != want || src.gotDay.Format(time.DateOnly) != want {
				t.Errorf("month end = %s / %s, want %s", got.MonthEnd.Format(time.DateOnly), src.gotDay.Format(time.DateOnly), want)
			}
			if tc.first != "" && got.Lines[0].MatterID != tc.first {
				t.Errorf("first line = %s, want %s", got.Lines[0].MatterID, tc.first)
			}
		})
	}
}

func TestMonthEnd(t *testing.T) {
	tests := []struct{ in, want string }{
		{"2026-01-01", "2026-01-31"},
		{"2028-02-10", "2028-02-29"},
		{"2026-12-31", "2026-12-31"},
	}
	for _, tc := range tests {
		d, err := time.Parse(time.DateOnly, tc.in)
		if err != nil {
			t.Fatal(err)
		}
		if got := MonthEnd(d).Format(time.DateOnly); got != tc.want {
			t.Errorf("MonthEnd(%s) = %s, want %s", tc.in, got, tc.want)
		}
	}
}
