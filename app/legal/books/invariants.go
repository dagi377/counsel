package books

import (
	"context"
	"fmt"
)

// Invariants is the books Reconciler. It compares ledger balances with the billing
// subledgers they must equal; the nightly reconciliation job feeds it a Snapshot.
type Invariants struct{}

var _ Reconciler = Invariants{}

// Reconcile returns every BIL-I-013 mismatch in s, with ErrMismatch when there is one.
// A mismatch names account codes and amounts only, never a client.
func (Invariants) Reconcile(ctx context.Context, s Snapshot) ([]Mismatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("reconcile: %w", err)
	}
	// rule: BIL-I-013
	checks := []Mismatch{
		{Rule: "BIL-I-013", Account: AcctARFees + "+" + AcctARInterest, LedgerCents: s.ARBalanceCents, SubCents: s.OpenBillBalancesCents},
		{Rule: "BIL-I-013", Account: AcctClientCredit, LedgerCents: s.ClientCreditLedger, SubCents: s.ClientCreditSubledger},
		{Rule: "BIL-I-013", Account: AcctWIP, LedgerCents: s.WIPLedgerCents, SubCents: s.UnbilledTimeValueCents},
	}
	var out []Mismatch
	for _, c := range checks {
		if c.LedgerCents != c.SubCents {
			out = append(out, c)
		}
	}
	if len(out) > 0 {
		return out, fmt.Errorf("reconcile: %d mismatches: %w", len(out), ErrMismatch)
	}
	return nil, nil
}
