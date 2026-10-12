package intake

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestIntakeGateEffects(t *testing.T) {
	type run func(context.Context, gates.Execer, registry.Approved) error
	lead := func(ctx context.Context, tx gates.Execer, g registry.Approved) error {
		return leadDecision(ctx, tx, g, efNow)
	}
	form := func(ctx context.Context, tx gates.Execer, g registry.Approved) error {
		return publicWording(ctx, tx, g, efNow)
	}
	const leadOK = `{"lead_id":"l-1","decision":"decline"}`
	const formOK = `{"form_id":"f-1","version":1}`
	tests := []struct {
		name    string
		fn      run
		g       registry.Approved
		rows    []int64
		wantSQL string
		wantErr error
	}{
		// rule: ARC-I-012
		{"G1 decision recorded", lead, efGate(GateLeadDecision, leadOK, efAgent, efLawyer), nil, "SET decision = $2", nil},
		{"G1 repeat re-matches the same decision", lead, efGate(GateLeadDecision, leadOK, efAgent, efLawyer), []int64{1}, "decision = $2 AND decided_by = $3::uuid", nil},
		{"G1 decided otherwise fails closed", lead, efGate(GateLeadDecision, leadOK, efAgent, efLawyer), []int64{0}, "intake.lead", gates.ErrNotApprovable},
		{"G1 unknown decision", lead, efGate(GateLeadDecision, `{"lead_id":"l-1","decision":"maybe"}`, efAgent, efLawyer), nil, "", gates.ErrNotApprovable},
		// rule: ARC-I-019
		{"G1 agent never approves", lead, efGate(GateLeadDecision, leadOK, efPrep, efAgent), nil, "", gates.ErrAgentApproves},
		{"G1 preparer never approves", lead, efGate(GateLeadDecision, leadOK, efAgent, efPrep), nil, "", gates.ErrPreparerApproves},
		{"G-form published", form, efGate(GatePublicWording, formOK, efAgent, efLawyer), nil, "status = 'published'", nil},
		{"G-form repeat is a no-op", form, efGate(GatePublicWording, formOK, efAgent, efLawyer), []int64{1}, "status = 'published' AND approved_by = $2::uuid", nil},
		{"G-form agent never approves", form, efGate(GatePublicWording, formOK, efPrep, efAgent), nil, "", gates.ErrAgentApproves},
		{"G-form preparer never approves", form, efGate(GatePublicWording, formOK, efPrep, efPrep), nil, "", gates.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &effectExec{rows: tt.rows}
			err := tt.fn(context.Background(), tx, tt.g)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantSQL == "" {
				if len(tx.sqls) != 0 && tt.wantErr != gates.ErrNotApprovable {
					t.Fatalf("ran %q, want nothing", tx.sqls)
				}
				return
			}
			if len(tx.sqls) == 0 || !strings.Contains(tx.sqls[0], tt.wantSQL) {
				t.Fatalf("ran %q, want %q", tx.sqls, tt.wantSQL)
			}
		})
	}
}
