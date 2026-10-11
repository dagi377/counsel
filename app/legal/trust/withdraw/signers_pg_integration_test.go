//go:build integration

package withdraw

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
)

// TestPGSignersIntegration runs PGSigners on the real schema: an unknown account and a
// malformed user id never sign. It needs COUNSEL_TEST_DB pointing at a migrated database.
func TestPGSignersIntegration(t *testing.T) {
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	for _, tc := range []struct {
		name, account, user string
	}{
		{"unknown account", "00000000-0000-7000-8000-000000000001", "00000000-0000-7000-8000-000000000002"},
		{"malformed user", "00000000-0000-7000-8000-000000000001", "not-a-uuid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			ok, err := PGSigners{}.IsSignatory(ctx, tx, trust.AccountID(tc.account), tc.user)
			if err != nil || ok {
				t.Fatalf("IsSignatory = %v, %v; want false, nil", ok, err)
			}
		})
	}
}
