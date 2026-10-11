package access

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestOffboardingEffect(t *testing.T) {
	const ok = `{"user_id":"u-leaver","handover":[{"matter_id":"m-1"}],"cutoff_at":"2026-10-12T17:00:00-04:00"}`
	tests := []struct {
		name        string
		prep, by    registry.Principal
		rows        []int64
		wantSQLs    int
		wantPublish int
		wantErr     error
	}{
		// rule: ARC-I-012
		{"leaver offboarded, user.deactivated published", efAgent, efLawyer, nil, 1, 1, nil},
		{"repeat finds them offboarded, publishes nothing", efAgent, efLawyer, []int64{0, 1}, 2, 0, nil},
		{"no such user fails closed", efAgent, efLawyer, []int64{0, 0}, 2, 0, gates.ErrNotApprovable},
		// rule: ARC-I-019
		{"agent never approves", efPrep, efAgent, nil, 0, 0, gates.ErrAgentApproves},
		{"preparer never approves", efAgent, efPrep, nil, 0, 0, gates.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &effectExec{rows: tt.rows}
			var published []identity.Deactivated
			err := offboard(context.Background(), tx, efGate(KindOffboarding, ok, tt.prep, tt.by), efNow,
				func(_ context.Context, b identity.Deactivated) error { published = append(published, b); return nil })
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(tx.sqls) != tt.wantSQLs || len(published) != tt.wantPublish {
				t.Fatalf("ran %d statements, published %d; want %d, %d", len(tx.sqls), len(published), tt.wantSQLs, tt.wantPublish)
			}
			if tt.wantPublish == 1 && (published[0].UserID != "u-leaver" || published[0].GateID != "g-1") {
				t.Fatalf("published %+v", published[0])
			}
		})
	}
}
