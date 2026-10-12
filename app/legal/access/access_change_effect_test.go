package access

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestAccessChangeEffect(t *testing.T) {
	const (
		ok       = `{"user_id":"u-1","change":{"role_id":"r-2"}}`
		grantOne = `{"capability_id":"trust.withdraw","level":"approve","reason":"cover","expires_at":"2026-12-31T00:00:00Z"}`
		grantTwo = `{"capability_id":"docs.read","level":"view","matter_id":"m-1","reason":"cover","expires_at":"2026-12-31T00:00:00Z"}`
		full     = `{"user_id":"u-1","change":{"role_id":"r-2","grants":[` + grantOne + `,` + grantTwo + `]}}`
		grants   = `{"user_id":"u-1","change":{"grants":[` + grantOne + `]}}`
		expired  = `{"user_id":"u-1","change":{"grants":[{"capability_id":"docs.read","level":"view","reason":"r","expires_at":"2026-10-01T00:00:00Z"}]}}`
		badLevel = `{"user_id":"u-1","change":{"grants":[{"capability_id":"docs.read","level":"none","reason":"r","expires_at":"2026-12-31T00:00:00Z"}]}}`
		noReason = `{"user_id":"u-1","change":{"grants":[{"capability_id":"docs.read","level":"view","expires_at":"2026-12-31T00:00:00Z"}]}}`
		dupGrant = `{"user_id":"u-1","change":{"grants":[` + grantOne + `,` + grantOne + `]}}`
		selfPerm = `{"user_id":"00000000-0000-0000-0000-00000000000a","change":{"role_id":"r-2"}}`
	)
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
		{"role and every capability grant apply", full, efAgent, efLawyer, nil, 3, nil},
		{"grants alone apply", grants, efAgent, efLawyer, nil, 1, nil},
		{"repeat finds role and grants already applied", full, efAgent, efLawyer, []int64{0, 1, 0, 1, 0, 1}, 6, nil},
		{"a grant the user may not take fails the whole set", full, efAgent, efLawyer, []int64{1, 0, 0}, 3, gates.ErrNotApprovable},
		{"expired grant refused", expired, efAgent, efLawyer, nil, 0, gates.ErrNotApprovable},
		{"unknown level refused", badLevel, efAgent, efLawyer, nil, 0, gates.ErrNotApprovable},
		{"grant without reason refused", noReason, efAgent, efLawyer, nil, 0, gates.ErrNotApprovable},
		{"duplicate grant refused", dupGrant, efAgent, efLawyer, nil, 0, gates.ErrNotApprovable},
		{"approver never changes their own access", selfPerm, efAgent, efLawyer, nil, 0, gates.ErrNotApprovable},
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
