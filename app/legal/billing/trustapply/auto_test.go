package trustapply

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
)

type proposer struct{ got []Proposal }

func (p *proposer) Propose(_ context.Context, pr Proposal) (string, error) {
	p.got = append(p.got, pr)
	return "p1", nil
}

// rule: BIL-C-052
func TestBIL_C_052_AutoApplyProposesOnly(t *testing.T) {
	tests := []struct {
		name     string
		flag     bool
		setting  bool
		bill     Bill
		avail    int64
		proposed bool
		amount   int64
		err      error
	}{
		{"setting on, bill approved: proposal exists", true, true, delivered(day(10)), 50000, true, 50000, nil},
		{"capped by bill balance less reserved", true, true, delivered(day(10)), 1_000_000, true, 70000, nil},
		{"setting off: no proposal", true, false, delivered(day(10)), 50000, false, 0, nil},
		{"no trust funds: no proposal", true, true, delivered(day(10)), 0, false, 0, nil},
		{"undelivered bill refused", true, true, delivered(time.Time{}), 50000, false, 0, ErrNotDelivered},
		{"flag off", false, true, delivered(day(10)), 50000, false, 0, ErrDisabled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &store{bill: tc.bill, avail: tc.avail}
			pr := &proposer{}
			a := AutoApplier{Applier: applier(t, s), Proposer: pr}
			ev := Approved{BillID: "b1", MatterID: "m1", TrustAccountID: "t1", AutoApply: tc.setting, On: day(10)}
			p, ok, err := a.OnApproved(legalflagstest.Context(t, FlagBylaw9Verified, tc.flag), ev)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if ok != tc.proposed || len(pr.got) != map[bool]int{true: 1, false: 0}[tc.proposed] {
				t.Fatalf("proposed = %v (%d rows), want %v", ok, len(pr.got), tc.proposed)
			}
			// Ledger unchanged: available trust and the bill balance are as before.
			if s.avail != tc.avail || s.bill != tc.bill {
				t.Fatalf("ledger or bill changed: avail %d bill %+v", s.avail, s.bill)
			}
			if !tc.proposed {
				return
			}
			if p.Status != StatusProposed || p.Transfer.AmountCents != tc.amount || p.ID != "p1" {
				t.Fatalf("proposal = %+v, want proposed %d", p, tc.amount)
			}
			if !slices.Equal(p.Gates, []string{"G-B4", "G11"}) {
				t.Fatalf("gates = %v, want G-B4, G11", p.Gates)
			}
		})
	}
}
