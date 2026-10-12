package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// autonomyExec records statements and answers rows affected per call (default 1).
type autonomyExec struct {
	rows []int64
	sqls []string
}

func (f *autonomyExec) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.sqls = append(f.sqls, sql)
	n := int64(1)
	if i := len(f.sqls) - 1; i < len(f.rows) {
		n = f.rows[i]
	}
	return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", n)), nil
}

func TestSkillAutonomyEffect(t *testing.T) {
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: "u-a"}
	prep := registry.Principal{Kind: registry.PrincipalUser, UserID: "u-b"}
	bot := registry.Principal{Kind: registry.PrincipalAgent, RunID: "r-1", ActingFor: "u-b"}
	const ok = `{"skill":"draft_email","autonomy":"draft"}`
	tests := []struct {
		name     string
		payload  string
		prep, by registry.Principal
		rows     []int64
		wantSQLs int
		wantErr  error
	}{
		// rule: ARC-I-012
		{"approval sets the autonomy", ok, bot, user, nil, 1, nil},
		{"repeat matches the same rows, no-op", ok, bot, user, []int64{1}, 1, nil},
		{"above the cap or unknown skill does not move", ok, bot, user, []int64{0}, 1, gates.ErrNotApprovable},
		{"unknown level refused", `{"skill":"draft_email","autonomy":"full"}`, bot, user, nil, 0, gates.ErrNotApprovable},
		// rule: ARC-I-019
		{"agent never approves", ok, prep, bot, nil, 0, gates.ErrAgentApproves},
		{"preparer never approves", ok, bot, prep, nil, 0, gates.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &autonomyExec{rows: tt.rows}
			g := registry.Approved{GateID: "g-1", Kind: GateSkillAutonomy, Payload: []byte(tt.payload), Preparer: tt.prep, Approvers: []registry.Principal{tt.by}}
			err := setSkillAutonomy(context.Background(), tx, g)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(tx.sqls) != tt.wantSQLs {
				t.Fatalf("ran %d statements, want %d", len(tx.sqls), tt.wantSQLs)
			}
		})
	}
}
