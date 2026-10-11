package books

import (
	"context"
	"errors"
	"math/rand/v2"
	"testing"
)

// sim drives random billing events through balanced operating-book entries and keeps the
// subledgers (open bill balances, client credit) next to the ledger they must match.
type sim struct {
	t      *testing.T
	ledger map[string]int64 // debit minus credit per account
	bills  []int64          // open balance per bill
	credit []int64          // credit per client
}

func (s *sim) post(lines ...Line) {
	s.t.Helper()
	e := Entry{Book: BookOperating, Lines: lines}
	if err := (Charts{}).CheckEntry(e); err != nil {
		s.t.Fatalf("entry %v: %v", e, err)
	}
	for _, l := range lines {
		s.ledger[l.Account] += l.DebitCents - l.CreditCents
	}
}

func (s *sim) step(r *rand.Rand) {
	switch op := r.IntN(5); {
	case op == 0 || len(s.bills) == 0: // issue a bill
		amt := 1 + r.Int64N(500_000)
		s.post(dr(AcctARFees, amt), cr(AcctFeeRevenue, amt))
		s.bills = append(s.bills, amt)
	case op == 1: // charge interest on a bill
		i, amt := r.IntN(len(s.bills)), 1+r.Int64N(5_000)
		s.post(dr(AcctARInterest, amt), cr(AcctInterestIncome, amt))
		s.bills[i] += amt
	case op == 2: // payment; any excess over the bill becomes client credit
		i, c := r.IntN(len(s.bills)), r.IntN(len(s.credit))
		pay := 1 + r.Int64N(600_000)
		applied := min(pay, s.bills[i])
		lines := []Line{dr(AcctOperatingBank, pay)}
		if applied > 0 {
			lines = append(lines, cr(AcctARFees, applied))
		}
		if pay > applied {
			lines = append(lines, cr(AcctClientCredit, pay-applied))
			s.credit[c] += pay - applied
		}
		s.post(lines...)
		s.bills[i] -= applied
	case op == 3: // apply client credit to a bill
		i, c := r.IntN(len(s.bills)), r.IntN(len(s.credit))
		amt := min(s.credit[c], s.bills[i])
		if amt == 0 {
			return
		}
		s.post(dr(AcctClientCredit, amt), cr(AcctARFees, amt))
		s.credit[c] -= amt
		s.bills[i] -= amt
	default: // refund client credit
		c := r.IntN(len(s.credit))
		if s.credit[c] == 0 {
			return
		}
		amt := 1 + r.Int64N(s.credit[c])
		s.post(dr(AcctClientCredit, amt), cr(AcctOperatingBank, amt))
		s.credit[c] -= amt
	}
}

func (s *sim) snapshot() Snapshot {
	var open, credit int64
	for _, b := range s.bills {
		open += b
	}
	for _, c := range s.credit {
		credit += c
	}
	return Snapshot{
		ARBalanceCents:        s.ledger[AcctARFees] + s.ledger[AcctARInterest],
		OpenBillBalancesCents: open,
		ClientCreditLedger:    -s.ledger[AcctClientCredit], // liability: credit balance
		ClientCreditSubledger: credit,
	}
}

// rule: BIL-I-013
func TestBIL_I_013_PropertyARAndClientCreditMatchSubledgers(t *testing.T) {
	for seed := range uint64(50) {
		r := rand.New(rand.NewPCG(seed, 13))
		s := &sim{t: t, ledger: map[string]int64{}, credit: make([]int64, 1+r.IntN(5))}
		for range 1 + r.IntN(300) {
			s.step(r)
		}
		snap := s.snapshot()
		got, err := Invariants{}.Reconcile(context.Background(), snap)
		if err != nil || len(got) != 0 {
			t.Fatalf("seed %d: Reconcile = %v, %v; snapshot %+v", seed, got, err, snap)
		}
	}
}

// rule: BIL-I-013
func TestBIL_I_013_ReconcileReportsEachMismatch(t *testing.T) {
	ok := Snapshot{ARBalanceCents: 100, OpenBillBalancesCents: 100, ClientCreditLedger: 50, ClientCreditSubledger: 50, WIPLedgerCents: 7, UnbilledTimeValueCents: 7}
	tests := []struct {
		name     string
		mutate   func(*Snapshot)
		accounts []string
	}{
		{"all match", func(*Snapshot) {}, nil},
		{"zero snapshot", func(s *Snapshot) { *s = Snapshot{} }, nil},
		{"AR off by one", func(s *Snapshot) { s.ARBalanceCents++ }, []string{"OP-1100+OP-1110"}},
		{"client credit short", func(s *Snapshot) { s.ClientCreditSubledger-- }, []string{AcctClientCredit}},
		{"WIP off", func(s *Snapshot) { s.WIPLedgerCents = 0 }, []string{AcctWIP}},
		{"AR and credit", func(s *Snapshot) { s.OpenBillBalancesCents = 0; s.ClientCreditLedger = -50 }, []string{"OP-1100+OP-1110", AcctClientCredit}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ok
			tt.mutate(&s)
			got, err := Invariants{}.Reconcile(context.Background(), s)
			if (len(tt.accounts) > 0) != errors.Is(err, ErrMismatch) {
				t.Fatalf("err = %v, want mismatch %v", err, len(tt.accounts) > 0)
			}
			if len(got) != len(tt.accounts) {
				t.Fatalf("got %d mismatches %+v, want %v", len(got), got, tt.accounts)
			}
			for i, m := range got {
				if m.Rule != "BIL-I-013" || m.Account != tt.accounts[i] || m.LedgerCents == m.SubCents {
					t.Errorf("mismatch %d = %+v, want account %s", i, m, tt.accounts[i])
				}
			}
		})
	}
}

func TestReconcileHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Invariants{}).Reconcile(ctx, Snapshot{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
