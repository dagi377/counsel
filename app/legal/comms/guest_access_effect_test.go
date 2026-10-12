package comms

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// guestExec records statements and answers rows affected per call (default 1).
type guestExec struct {
	rows []int64
	sqls []string
}

func (f *guestExec) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.sqls = append(f.sqls, sql)
	n := int64(1)
	if i := len(f.sqls) - 1; i < len(f.rows) {
		n = f.rows[i]
	}
	return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", n)), nil
}

func TestGuestAccessEffect(t *testing.T) {
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: "u-a"}
	prep := registry.Principal{Kind: registry.PrincipalUser, UserID: "u-b"}
	bot := registry.Principal{Kind: registry.PrincipalAgent, RunID: "r-1", ActingFor: "u-b"}
	now := func() time.Time { return time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC) }
	const ok = `{"matter_id":"m-1","guest_contact_id":"c-1"}`
	tests := []struct {
		name     string
		payload  string
		prep, by registry.Principal
		rows     []int64
		wantSQLs int
		wantErr  error
	}{
		// rule: ARC-I-012
		{"guest gets portal access", ok, bot, user, nil, 1, nil},
		{"repeat finds the guest row, inserts nothing", ok, bot, user, []int64{0}, 1, nil},
		{"no guest refused", `{"matter_id":"m-1"}`, bot, user, nil, 0, gates.ErrNotApprovable},
		// rule: ARC-I-019
		{"agent never approves", ok, prep, bot, nil, 0, gates.ErrAgentApproves},
		{"preparer never approves", ok, bot, prep, nil, 0, gates.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &guestExec{rows: tt.rows}
			g := registry.Approved{GateID: "g-1", Kind: GateGuestAccess, Payload: []byte(tt.payload), Preparer: tt.prep, Approvers: []registry.Principal{tt.by}}
			err := grantGuest(context.Background(), tx, g, now)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(tx.sqls) != tt.wantSQLs {
				t.Fatalf("ran %d statements, want %d", len(tx.sqls), tt.wantSQLs)
			}
		})
	}
}
