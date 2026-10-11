package access

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestAccessChangeEffect(t *testing.T) {
	const ok = `{"user_id":"u-1","change":{"role_id":"r-2"}}`
	tests := []struct {
		name     string
		payload  string
		prep, by registry.Principal
		rows     []int64
		wantSQLs int
		wantErr  error
	}{
		// rule: ARC-I-012
		{"user takes the approved role", ok, efAgent, efLawyer, nil, 1, nil},
		{"repeat finds the user on the role", ok, efAgent, efLawyer, []int64{0, 1}, 2, nil},
		{"inactive role, offboarded or unknown user fails closed", ok, efAgent, efLawyer, []int64{0, 0}, 2, gates.ErrNotApprovable},
		{"unknown change shape refused", `{"user_id":"u-1","change":{"role_id":"r-2","mfa":false}}`, efAgent, efLawyer, nil, 0, gates.ErrNotApprovable},
		{"empty change refused", `{"user_id":"u-1","change":{}}`, efAgent, efLawyer, nil, 0, gates.ErrNotApprovable},
		// rule: ARC-I-019
		{"agent never approves", ok, efPrep, efAgent, nil, 0, gates.ErrAgentApproves},
		{"preparer never approves", ok, efAgent, efPrep, nil, 0, gates.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &effectExec{rows: tt.rows}
			err := changeAccess(context.Background(), tx, efGate(KindAccessChange, tt.payload, tt.prep, tt.by), efNow)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(tx.sqls) != tt.wantSQLs {
				t.Fatalf("ran %d statements, want %d", len(tx.sqls), tt.wantSQLs)
			}
		})
	}
}
