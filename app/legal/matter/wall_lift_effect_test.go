package matter

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
// rule: ARC-I-042
func TestWallLiftEffect(t *testing.T) {
	const wall, person = `{"wall_id":"w-1"}`, `{"wall_id":"w-1","user_id":"00000000-0000-0000-0000-0000000000e1"}`
	L, S, P, A := gatestest.Lawyer, gatestest.Second, gatestest.Prep, gatestest.Agent
	screened := registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-0000000000e1"}
	tests := []struct {
		name    string
		g       registry.Approved
		min     int
		rows    []int64
		wantSQL string
		wantErr error
	}{
		{"wall lifted and audited", gatestest.Gate(KindWallLift, wall, P, L), 1, nil, "status = 'lifted'", nil},
		{"one person unscreened", gatestest.Gate(KindWallLift, person, P, L), 1, nil, "DELETE FROM matter.ethical_wall_screened_user", nil},
		{"repeat is a no-op", gatestest.Gate(KindWallLift, wall, P, L), 1, []int64{0, 1}, "SET gate_id = gate_id", nil},
		{"screened approver or lifted wall fails closed", gatestest.Gate(KindWallLift, wall, P, L), 1, []int64{0, 0}, "", gates.ErrNotApprovable},
		{"two approvers when the spec says so", gatestest.Gate(KindWallLift, wall, P, L, S), 2, nil, "matter.ethical_wall_lift", nil},
		{"one approver short of the spec", gatestest.Gate(KindWallLift, wall, P, L), 2, nil, "", gates.ErrNotApprovable},
		{"the screened person never approves", gatestest.Gate(KindWallLift, person, P, screened), 1, nil, "", gates.ErrPreparerApproves},
		{"agent never approves", gatestest.Gate(KindWallLift, wall, P, A), 1, nil, "", gates.ErrAgentApproves},
		{"preparer never approves", gatestest.Gate(KindWallLift, wall, P, P), 1, nil, "", gates.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &gatestest.Tx{Rows: tt.rows}
			err := liftWall(context.Background(), tx, tt.g, gatestest.Now, tt.min)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantSQL != "" && !tx.Ran(tt.wantSQL) {
				t.Fatalf("ran %q, want %q", tx.SQLs, tt.wantSQL)
			}
			if tt.wantErr == nil && !tx.Ran("INSERT INTO matter.ethical_wall_lift") {
				t.Fatalf("ran %q, want the audited lift record", tx.SQLs)
			}
		})
	}
}
