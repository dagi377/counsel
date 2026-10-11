package work

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestWorkGateEffects(t *testing.T) {
	type run func(context.Context, gates.Execer, registry.Approved) error
	out := func(ctx context.Context, tx gates.Execer, g registry.Approved) error {
		return outputDone(ctx, tx, g, efNow)
	}
	rule := enableAutomation
	const item = `{"item_id":"i-1"}`
	const ar = `{"rule_id":"r-1"}`
	tests := []struct {
		name    string
		fn      run
		g       registry.Approved
		rows    []int64
		wantSQL string
		wantErr error
	}{
		{"G-W1 item moves to done", out, efGate(GateWorkOutput, item, efAgent, efLawyer), nil, "s.status_group = 'done'", nil},
		{"G-W1 repeat leaves a done item", out, efGate(GateWorkOutput, item, efAgent, efLawyer), []int64{1}, "cur.status_group = 'done' OR", nil},
		{"G-W1 no path to done fails closed", out, efGate(GateWorkOutput, item, efAgent, efLawyer), []int64{0}, "work.work_item", gates.ErrNotApprovable},
		// rule: ARC-I-019
		{"G-W1 agent never approves", out, efGate(GateWorkOutput, item, efPrep, efAgent), nil, "", gates.ErrAgentApproves},
		{"G-W1 preparer never approves", out, efGate(GateWorkOutput, item, efAgent, efPrep), nil, "", gates.ErrPreparerApproves},
		{"G-AR rule enabled", rule, efGate(GateAutomation, ar, efAgent, efLawyer), nil, "SET enabled = true", nil},
		{"G-AR repeat is a no-op", rule, efGate(GateAutomation, ar, efAgent, efLawyer), []int64{1}, "SET enabled = true", nil},
		{"G-AR agent never approves", rule, efGate(GateAutomation, ar, efPrep, efAgent), nil, "", gates.ErrAgentApproves},
		{"G-AR preparer never approves", rule, efGate(GateAutomation, ar, efPrep, efPrep), nil, "", gates.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &effectExec{rows: tt.rows}
			err := tt.fn(context.Background(), tx, tt.g)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantSQL == "" {
				if len(tx.sqls) != 0 {
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
