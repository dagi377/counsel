//go:build integration

package withdraw

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	kaudit "github.com/dagi377/counsel/kernel/audit"
)

// TestAuditLogIntegration writes the solo-exception event through the kernel log on the
// migrated schema: the row commits with the caller's tx and vanishes when the tx rolls back.
func TestAuditLogIntegration(t *testing.T) {
	env := newItEnv(t, 100000)
	wid, _ := env.request(t, 1000)
	a := NewAuditLog(kaudit.New(nil, nil, kaudit.Options{Streams: map[string]string{"trust": "trust"}}))
	tests := []struct {
		name     string
		rollback bool
		want     int
	}{
		{name: "commits with tx", want: 1},
		{name: "rolls back with tx", rollback: true, want: 1},
	}
	errAbort := errors.New("abort")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			err := env.pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				if err := a.Record(ctx, tx, AuditSoloException, wid, env.users["lawyer1"]); err != nil {
					return err
				}
				if tt.rollback {
					return errAbort
				}
				return nil
			})
			if tt.rollback != errors.Is(err, errAbort) || (!tt.rollback && err != nil) {
				t.Fatalf("tx: %v", err)
			}
			var n int
			var detail string
			err = env.pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT count(*), coalesce(max(detail_masked::text), '') FROM audit.audit_event
WHERE action = $1 AND object_type = $2 AND object_id = $3::uuid AND actor_user_id = $4::uuid`,
					AuditSoloException, GateKind, wid, env.users["lawyer1"]).Scan(&n, &detail)
			})
			if err != nil {
				t.Fatal(err)
			}
			if n != tt.want {
				t.Fatalf("rows = %d, want %d", n, tt.want)
			}
			if detail != "{}" {
				t.Fatalf("detail = %s, want {} (no amounts or account numbers)", detail)
			}
		})
	}
}
