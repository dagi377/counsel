package trust

import (
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestCashGateEffect(t *testing.T) {
	clerk := registry.Principal{Kind: registry.PrincipalUser, UserID: "clerk"}
	lawyer := registry.Principal{Kind: registry.PrincipalUser, UserID: "lawyer"}
	agent := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "clerk"}
	ok := `{"matter_id":"m-1","amount_cents":800000,"aggregate_cents":800000,"exception":"from_financial_institution"}`
	tests := []struct {
		name     string
		flagOn   bool
		payload  string
		preparer registry.Principal
		approver registry.Principal
		want     error
	}{
		{"approval with an exception stands", true, ok, clerk, lawyer, nil},
		// rule: ARC-I-019
		{"agent refused", true, ok, clerk, agent, ErrAgentApproval},
		// rule: ARC-I-027
		{"preparer refused", true, ok, clerk, clerk, ErrSelfApproval},
		{"agent's acting user refused", true, ok, agent, clerk, ErrSelfApproval},
		// rule: BIL-L-039 (verify)
		{"no exception code", true, `{"matter_id":"m-1","amount_cents":800000,"aggregate_cents":800000}`, clerk, lawyer, ErrCashLimit},
		{"unknown exception code", true, `{"matter_id":"m-1","amount_cents":800000,"aggregate_cents":800000,"exception":"gift"}`, clerk, lawyer, ErrCashLimit},
		{"amount over aggregate", true, `{"matter_id":"m-1","amount_cents":800001,"aggregate_cents":800000,"exception":"bail"}`, clerk, lawyer, ErrInvalid},
		{"missing amount", true, `{"matter_id":"m-1","aggregate_cents":800000,"exception":"bail"}`, clerk, lawyer, ErrInvalid},
		{"by-law 9 flag off fails closed", false, ok, clerk, lawyer, ErrUnverified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := legalflagstest.Context(t, FlagBylaw9Verified, tt.flagOn)
			g := registry.Approved{GateID: "g-1", Kind: CashGateKind, Payload: []byte(tt.payload),
				Preparer: tt.preparer, Approvers: []registry.Principal{tt.approver}}
			if err := CashGateEffect(ctx, nil, g); !errors.Is(err, tt.want) {
				t.Fatalf("apply = %v, want %v", err, tt.want)
			}
			// rule: ARC-I-012
			// The effect writes nothing, so a repeat answers the same.
			if err := CashGateEffect(ctx, nil, g); !errors.Is(err, tt.want) {
				t.Fatalf("repeat = %v, want %v", err, tt.want)
			}
		})
	}
}
