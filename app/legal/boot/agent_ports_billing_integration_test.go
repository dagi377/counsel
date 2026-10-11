//go:build integration

package boot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/migrations"
	"github.com/dagi377/counsel/migrations/migrationstest"
)

// billingPortsDB creates a fresh migrated database on COUNSEL_TEST_DB and returns a pool on it.
func billingPortsDB(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "boot_billing_ports_" + hex.EncodeToString(b)
	defer migrationstest.LockClusterRoles(ctx, t, admin)()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	ccfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ccfg.Database = name
	conn, err := pgx.ConnectConfig(ctx, ccfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, conn, ""); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close(ctx)
	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pcfg.ConnConfig.Database = name
	pool, err := db.OpenConfig(ctx, pcfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestBillingAgentPortsPostgres reads through the wired Postgres ports on an empty firm:
// unknown bills fail closed and unknown matters have no accounts, time or WIP.
func TestBillingAgentPortsPostgres(t *testing.T) {
	pool := billingPortsDB(t)
	src, err := NewBillingAgentSources(pool, "firm-general")
	if err != nil {
		t.Fatal(err)
	}
	var (
		p AgentPorts
		w agent.WriteDeps
	)
	if err := WireBillingAgentPorts(src, &p, &w); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const unknown = "00000000-0000-4000-8000-00000000dead"
	tests := []struct {
		name    string
		read    func() (int, error)
		wantN   int
		wantErr error
	}{
		{"unknown bill balance is not found", func() (int, error) {
			_, err := p.BillBalances.BalanceCents(ctx, unknown)
			return 0, err
		}, 0, bill.ErrNotFound},
		{"unknown fee bill is not found", func() (int, error) {
			_, err := w.Bills.FeeBill(ctx, unknown, unknown)
			return 0, err
		}, 0, bill.ErrNotFound},
		{"unknown matter has no trust accounts", func() (int, error) {
			got, err := p.TrustAccounts.TrustAccountsOf(ctx, unknown)
			return len(got), err
		}, 0, nil},
		{"unknown matter has no time", func() (int, error) {
			got, err := p.MatterTime.MatterTime(ctx, unknown)
			return len(got), err
		}, 0, nil},
		{"unknown matter has no WIP", func() (int, error) {
			got, err := p.UnbilledWIP.UnbilledEntryIDs(ctx, unknown, clock.Date{Year: 2026, Month: 9, Day: 30})
			return len(got), err
		}, 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, err := tc.read()
			if !errors.Is(err, tc.wantErr) || n != tc.wantN {
				t.Fatalf("got %d, %v; want %d, %v", n, err, tc.wantN, tc.wantErr)
			}
		})
	}
}
