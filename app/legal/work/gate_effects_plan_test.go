package work

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/gates/gatestest"
	"github.com/dagi377/counsel/kernel/registry"
)

// rule: ARC-I-012
// rule: ARC-I-016
// rule: ARC-I-019
// rule: ARC-I-014
func TestWorkPlanGateEffects(t *testing.T) {
	type run func(context.Context, gates.Execer, registry.Approved) error
	now := gatestest.Now
	plan := func(ctx context.Context, tx gates.Execer, g registry.Approved) error {
		return acceptPlan(ctx, tx, g, now)
	}
	pub := func(ctx context.Context, tx gates.Execer, g registry.Approved) error {
		return publishWorkflow(ctx, tx, g, now)
	}
	sync := func(ctx context.Context, tx gates.Execer, g registry.Approved) error {
		return externalSync(ctx, tx, g, now)
	}
	abs := func(ctx context.Context, tx gates.Execer, g registry.Approved) error {
		return approveAbsence(ctx, tx, g, now)
	}
	const (
		planOK = `{"matter_id":"m-1","template_id":"t-1"}`
		pubOK  = `{"workflow_id":"w-1","version":2}`
		syncOK = `{"matter_id":"m-1","integration_id":"i-1"}`
		absOK  = `{"user_id":"00000000-0000-0000-0000-0000000000d1","start_on":"2026-11-02","end_on":"2026-11-06"}`
	)
	gt := gatestest.Gate
	L, S, P, A := gatestest.Lawyer, gatestest.Second, gatestest.Prep, gatestest.Agent
	absentee := registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-0000000000d1"}
	tests := []struct {
		name    string
		fn      run
		g       registry.Approved
		rows    []int64
		wantSQL string
		wantErr error
	}{
		{"G-W2 accepting creates the proposed tasks", plan, gt(GateWorkPlan, planOK, A, L), nil, "INSERT INTO work.work_item", nil},
		{"G-W2 repeat creates nothing", plan, gt(GateWorkPlan, planOK, A, L), []int64{0, 1}, "ON CONFLICT (gate_id) DO NOTHING", nil},
		{"G-W2 unknown template fails closed", plan, gt(GateWorkPlan, planOK, A, L), []int64{0, 0}, "", gates.ErrNotApprovable},
		{"G-W2 agent never approves", plan, gt(GateWorkPlan, planOK, P, A), nil, "", gates.ErrAgentApproves},
		{"G-W2 preparer never approves", plan, gt(GateWorkPlan, planOK, A, P), nil, "", gates.ErrPreparerApproves},
		{"G-W3 version released", pub, gt(GateWorkflowPublish, pubOK, P, L), nil, "INSERT INTO work.workflow_release", nil},
		{"G-W3 repeat re-matches the release", pub, gt(GateWorkflowPublish, pubOK, P, L), []int64{0, 1}, "gate_id = $2::uuid", nil},
		{"G-W3 stale version is a conflict", pub, gt(GateWorkflowPublish, pubOK, P, L), []int64{0, 0, 1}, "version <> $2", gates.ErrStale},
		{"G-W3 agent never approves", pub, gt(GateWorkflowPublish, pubOK, P, A), nil, "", gates.ErrAgentApproves},
		{"G-W3 preparer never approves", pub, gt(GateWorkflowPublish, pubOK, P, P), nil, "", gates.ErrPreparerApproves},
		{"G-W4 sync enabled by two", sync, gt(GateExternalSync, syncOK, P, L, S), nil, "INSERT INTO work.external_sync", nil},
		{"G-W4 repeat is a no-op", sync, gt(GateExternalSync, syncOK, P, L, S), []int64{0}, "ON CONFLICT DO NOTHING", nil},
		{"G-W4 one approver is not enough", sync, gt(GateExternalSync, syncOK, P, L), nil, "", gates.ErrNotApprovable},
		{"G-W4 agent never approves", sync, gt(GateExternalSync, syncOK, P, L, A), nil, "", gates.ErrAgentApproves},
		{"G-W4 preparer never approves", sync, gt(GateExternalSync, syncOK, P, L, P), nil, "", gates.ErrPreparerApproves},
		{"G-W5 absence recorded", abs, gt(GateAbsence, absOK, P, L), nil, "INSERT INTO work.absence", nil},
		{"G-W5 repeat is a no-op", abs, gt(GateAbsence, absOK, P, L), []int64{0}, "ON CONFLICT (gate_id)", nil},
		{"G-W5 reversed dates fail closed", abs, gt(GateAbsence, `{"user_id":"u","start_on":"2026-11-06","end_on":"2026-11-02"}`, P, L), nil, "", gates.ErrNotApprovable},
		{"G-W5 the absent person never approves", abs, gt(GateAbsence, absOK, P, absentee), nil, "", gates.ErrPreparerApproves},
		{"G-W5 agent never approves", abs, gt(GateAbsence, absOK, P, A), nil, "", gates.ErrAgentApproves},
		{"G-W5 preparer never approves", abs, gt(GateAbsence, absOK, P, P), nil, "", gates.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &gatestest.Tx{Rows: tt.rows}
			err := tt.fn(context.Background(), tx, tt.g)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantSQL != "" && !tx.Ran(tt.wantSQL) {
				t.Fatalf("ran %q, want %q", tx.SQLs, tt.wantSQL)
			}
			if tt.wantErr != nil && tt.wantErr != gates.ErrNotApprovable && tt.wantErr != gates.ErrStale && len(tx.SQLs) != 0 {
				t.Fatalf("refused approval ran %q", tx.SQLs)
			}
		})
	}
}
