package intake

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/registry"
)

// effectExec is a fake tx: it records statements and answers rows affected per call.
type effectExec struct {
	rows []int64
	sqls []string
	args [][]any
}

func (f *effectExec) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.sqls, f.args = append(f.sqls, sql), append(f.args, args)
	n := int64(1)
	if i := len(f.sqls) - 1; i < len(f.rows) {
		n = f.rows[i]
	}
	return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", n)), nil
}

var (
	efLawyer = registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-00000000000a"}
	efPrep   = registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-00000000000b"}
	efAgent  = registry.Principal{Kind: registry.PrincipalAgent, RunID: "r-1", ActingFor: efPrep.UserID}
	efNow    = func() time.Time { return time.Date(2026, 10, 11, 23, 30, 0, 0, time.UTC) }
)

func efGate(kind, payload string, prep registry.Principal, approvers ...registry.Principal) registry.Approved {
	return registry.Approved{GateID: "g-1", Kind: kind, Payload: []byte(payload), Preparer: prep, Approvers: approvers}
}
