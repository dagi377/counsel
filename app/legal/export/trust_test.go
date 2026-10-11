package export

import (
	"encoding/csv"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func tdate(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation(time.DateOnly, s, toronto)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// rule: MIG-073
func TestMIG_073_TrustBalancesReconcile(t *testing.T) {
	lines := []TrustLine{
		{AccountID: "TA-1", LedgerID: "L-1", Date: tdate(t, "2027-01-05"), Cents: 3_000_000},
		{AccountID: "TA-1", LedgerID: "L-2", Date: tdate(t, "2027-01-10"), Cents: 2_000_000},
		{AccountID: "TA-1", LedgerID: "L-2", Date: tdate(t, "2027-01-20"), Cents: -125_000},
		{AccountID: "TA-1", LedgerID: "L-1", Date: tdate(t, "2027-02-02"), Cents: 50_000},
	}
	signed := func(cents int64) []SignedReconciliation {
		return []SignedReconciliation{
			{ID: "R-2026-12", AccountID: "TA-1", MonthEnd: tdate(t, "2026-12-31"), ClientListingCents: 0},
			{ID: "R-2027-01", AccountID: "TA-1", MonthEnd: tdate(t, "2027-01-31"), ClientListingCents: cents},
		}
	}
	tests := []struct {
		name    string
		asOf    string
		recs    []SignedReconciliation
		total   int64
		lastRec string
		wantErr error
	}{
		{"month end matches signed", "2027-01-31", signed(4_875_000), 4_875_000, "R-2027-01", nil},
		{"month end mismatch", "2027-01-31", signed(4_900_000), 0, "", ErrTrustMismatch},
		{"mid month not compared", "2027-02-15", signed(4_900_000), 4_925_000, "R-2027-01", nil},
		{"no reconciliation", "2027-01-31", nil, 4_875_000, "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TrustBalances(tdate(t, tc.asOf), lines, tc.recs)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if len(got.Balances) != 1 || got.Balances[0].TotalCents != tc.total || got.Balances[0].Currency != "CAD" {
				t.Fatalf("balances = %+v", got.Balances)
			}
			last := ""
			if p := got.Balances[0].LastSignedReconciliation; p != nil {
				last = *p
			}
			if last != tc.lastRec {
				t.Errorf("last signed = %q, want %q", last, tc.lastRec)
			}
			recs, err := csv.NewReader(strings.NewReader(string(got.CSV))).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			var sum int64
			for _, r := range recs[1:] {
				n, err := strconv.ParseInt(r[2], 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				sum += n
				if r[4] != tc.asOf {
					t.Errorf("as_of = %s", r[4])
				}
			}
			if sum != tc.total {
				t.Errorf("csv sum = %d, want %d", sum, tc.total)
			}
		})
	}
}

func TestTrustBalancesBadInput(t *testing.T) {
	tests := []struct {
		name  string
		lines []TrustLine
		recs  []SignedReconciliation
	}{
		{"line without ledger", []TrustLine{{AccountID: "TA-1"}}, nil},
		{"reconciliation without id", nil, []SignedReconciliation{{AccountID: "TA-1"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := TrustBalances(tdate(t, "2027-01-31"), tc.lines, tc.recs); !errors.Is(err, ErrBadTrustInput) {
				t.Errorf("err = %v", err)
			}
		})
	}
}
