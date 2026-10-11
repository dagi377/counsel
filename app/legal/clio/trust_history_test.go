package clio

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/flags"
)

func historyCtx(t *testing.T, on bool) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: historyFlag}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(historyFlag, on); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}

// fortyLines returns 40 historical lines on ledger summing to total.
func fortyLines(ledger string, total int64) []HistoricalLine {
	out := make([]HistoricalLine, 40)
	for i := range out {
		out[i] = HistoricalLine{ID: "H", LedgerID: ledger, Kind: "deposit", Method: "unknown",
			TxnDate: clock.Date{Year: 2026, Month: time.September, Day: 1}, AmountCents: total / 40}
	}
	return out
}

// rule: MIG-037 (verify)
func TestMIG_037_HistoryNeverChangesLiveBalance(t *testing.T) {
	opening := LiveTxn{ID: "T-1", LedgerID: "L-1", Kind: "opening_balance", AmountCents: 3_000_000}
	late := HistoricalLine{ID: "H-late", LedgerID: "L-1", Kind: "deposit", AmountCents: 999,
		TxnDate: clock.Date{Year: 2026, Month: time.November, Day: 2}}
	tests := []struct {
		name        string
		flagOn      bool
		book        TrustBook
		wantLedger  int64
		wantAccount int64
		wantHistory int64
		wantErr     error
	}{
		{"flag off", false, TrustBook{Live: []LiveTxn{opening}}, 0, 0, 0, ErrHistoryUnverified},
		{"40 historical lines plus opening row count once", true,
			TrustBook{Live: []LiveTxn{opening}, History: fortyLines("L-1", 3_000_000)},
			3_000_000, 3_000_000, 3_000_000, nil},
		{"history added after cut-over changes nothing live", true,
			TrustBook{Live: []LiveTxn{opening}, History: append(fortyLines("L-1", 3_000_000), late)},
			3_000_000, 3_000_000, 3_000_999, nil},
		{"live correction moves the balance", true,
			TrustBook{Live: []LiveTxn{opening, {ID: "T-2", LedgerID: "L-1", Kind: "receipt", AmountCents: 1_000}},
				History: fortyLines("L-1", 3_000_000)},
			3_001_000, 3_001_000, 3_000_000, nil},
		{"history only, no live rows", true, TrustBook{History: fortyLines("L-1", 3_000_000)}, 0, 0, 3_000_000, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := historyCtx(t, tt.flagOn)
			got, err := tt.book.LedgerBalance(ctx, "L-1")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ledger err = %v, want %v", err, tt.wantErr)
			}
			acct, err := tt.book.AccountBalance(ctx)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("account err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got != tt.wantLedger || acct != tt.wantAccount {
				t.Fatalf("ledger = %d, account = %d, want %d, %d", got, acct, tt.wantLedger, tt.wantAccount)
			}
			if h := tt.book.HistoryTotal("L-1"); h != tt.wantHistory {
				t.Fatalf("history = %d, want %d", h, tt.wantHistory)
			}
		})
	}
}
