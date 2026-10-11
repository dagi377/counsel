//go:build integration

package sync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/migrations/migrationstest"
)

// syncDB creates a database on COUNSEL_TEST_DB and applies the init migration.
func syncDB(t *testing.T) *db.Pool {
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
	name := "clio_sync_notifier_" + hex.EncodeToString(b)
	defer migrationstest.LockClusterRoles(ctx, t, admin)()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.ConnConfig.Database = name
	pool, err := db.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(pool.Close)
	ddl, err := os.ReadFile(filepath.FromSlash("../../../../migrations/0001_init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	_, err = conn.PgConn().Exec(ctx, string(ddl)).ReadAll()
	_ = conn.Close(ctx)
	if err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	return pool
}

// noPrefs is a preference store with no stored rows: every channel is on.
type noPrefs struct{}

func (noPrefs) Load(context.Context, pgx.Tx, string, string) ([]notify.Preference, error) {
	return nil, nil
}

func (noPrefs) Save(context.Context, notify.Preference) error { return nil }

// rule: MIG-042
// rule: ARC-I-049
func TestMIG_042_OutboxNotifierIntegration(t *testing.T) {
	pool := syncDB(t)
	ctx := context.Background()
	out := outbox.NewPG(pool)
	svc := notify.New(noPrefs{}, out)
	if err := svc.RegisterTemplate(notify.Template{ID: testTemplate, Channels: []notify.Channel{notify.InApp, notify.Email},
		Subject: "{{subscription_id}}", Body: "{{source}} {{expires_at}}"}); err != nil {
		t.Fatal(err)
	}
	recipients := []string{"00000000-0000-7000-8000-0000000000a1", "00000000-0000-7000-8000-0000000000a2"}
	n, err := NewOutboxNotifier(pool, out, svc, testTemplate, recipients)
	if err != nil {
		t.Fatal(err)
	}
	f := RenewalFailure{SubscriptionID: "00000000-0000-7000-8000-000000000051", Source: "clio",
		ExpiresAt: time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)}
	tests := []struct {
		name  string
		run   func() error
		topic string
		want  int
	}{
		{"emit writes one renewal_failed row", func() error { return n.Emit(ctx, EventRenewalFailed, f) }, EventRenewalFailed, 1},
		{"alert writes one request per recipient and channel", func() error { return n.Alert(ctx, f) }, notify.EventRequested, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err != nil {
				t.Fatal(err)
			}
			var got int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform.outbox WHERE topic = $1`, tc.topic).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("outbox rows for %s = %d, want %d", tc.topic, got, tc.want)
			}
		})
	}
}
