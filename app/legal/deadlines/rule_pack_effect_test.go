package deadlines

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// rulePackExec records statements and answers rows affected per call (default 1).
type rulePackExec struct {
	rows []int64
	sqls []string
}

func (f *rulePackExec) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.sqls = append(f.sqls, sql)
	n := int64(1)
	if i := len(f.sqls) - 1; i < len(f.rows) {
		n = f.rows[i]
	}
	return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", n)), nil
}

func TestRulePackEffect(t *testing.T) {
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: "u-a"}
	prep := registry.Principal{Kind: registry.PrincipalUser, UserID: "u-b"}
	bot := registry.Principal{Kind: registry.PrincipalAgent, RunID: "r-1", ActingFor: "u-b"}
	const ok = `{"pack":"ON-RCP","version":3,"province":"ON"}`
	tests := []struct {
		name     string
		payload  string
		prep, by registry.Principal
		rows     []int64
		wantSQLs int
		wantErr  error
	}{
		// rule: ARC-I-012
		{"verified version activates, others retire", ok, bot, user, nil, 2, nil},
		{"repeat finds it active and nothing to retire", ok, bot, user, []int64{1, 0}, 2, nil},
		{"draft or missing version does not activate", ok, bot, user, []int64{0}, 1, gates.ErrNotApprovable},
		{"version below 1 refused", `{"pack":"ON-RCP","version":0,"province":"ON"}`, bot, user, nil, 0, gates.ErrNotApprovable},
		// rule: ARC-I-019
		{"agent never approves", ok, prep, bot, nil, 0, gates.ErrAgentApproves},
		{"preparer never approves", ok, bot, prep, nil, 0, gates.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &rulePackExec{rows: tt.rows}
			g := registry.Approved{GateID: "g-1", Kind: GateRulePack, Payload: []byte(tt.payload), Preparer: tt.prep, Approvers: []registry.Principal{tt.by}}
			err := activateRulePack(context.Background(), tx, g)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(tx.sqls) != tt.wantSQLs {
				t.Fatalf("ran %d statements, want %d", len(tx.sqls), tt.wantSQLs)
			}
		})
	}
}
