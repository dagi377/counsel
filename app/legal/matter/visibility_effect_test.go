package matter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestMatterVisibilityEffect(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		prep, by registry.Principal
		rows     []int64
		wantCol  string
		wantErr  error
	}{
		// rule: ARC-I-042
		{"restricted", `{"matter_id":"m-1","visibility":"restricted"}`, efAgent, efLawyer, nil, "restricted", nil},
		{"team", `{"matter_id":"m-1","visibility":"team"}`, efAgent, efLawyer, nil, "team_only", nil},
		{"firm", `{"matter_id":"m-1","visibility":"firm"}`, efAgent, efLawyer, nil, "firm_open", nil},
		{"repeat is a no-op", `{"matter_id":"m-1","visibility":"firm"}`, efAgent, efLawyer, []int64{1}, "firm_open", nil},
		{"missing matter fails closed", `{"matter_id":"m-1","visibility":"firm"}`, efAgent, efLawyer, []int64{0}, "firm_open", gates.ErrNotApprovable},
		{"named team not supported", `{"matter_id":"m-1","visibility":"team","team_user_ids":["u-1"]}`, efAgent, efLawyer, nil, "", gates.ErrNotApprovable},
		// rule: ARC-I-019
		{"agent never approves", `{"matter_id":"m-1","visibility":"firm"}`, efPrep, efAgent, nil, "", gates.ErrAgentApproves},
		{"preparer never approves", `{"matter_id":"m-1","visibility":"firm"}`, efAgent, efPrep, nil, "", gates.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &effectExec{rows: tt.rows}
			err := matterVisibility(context.Background(), tx, efGate(KindMatterVisibility, tt.payload, tt.prep, tt.by), efNow)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantCol == "" {
				if len(tx.sqls) != 0 {
					t.Fatalf("ran %q, want nothing", tx.sqls)
				}
				return
			}
			if !strings.Contains(tx.sqls[0], "SET visibility = $2") || tx.args[0][1] != tt.wantCol {
				t.Fatalf("ran %q with %v, want visibility %s", tx.sqls[0], tx.args[0], tt.wantCol)
			}
		})
	}
}
