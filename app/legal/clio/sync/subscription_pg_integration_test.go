//go:build integration

package sync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/migrations"
)

// subscriptionDB creates a migrated database on COUNSEL_TEST_DB and returns a pool on it.
func subscriptionDB(t *testing.T) *db.Pool {
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
	name := "clio_subscription_" + hex.EncodeToString(b)
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
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	err = migrations.Apply(ctx, conn, "")
	_ = conn.Close(ctx)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	pool, err := db.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestPGSubscriptionsIntegration(t *testing.T) {
	pool := subscriptionDB(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	ids := map[string]string{}
	err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for _, s := range []struct {
			key     string
			expires time.Time
		}{{"soon", base}, {"later", base.Add(30 * 24 * time.Hour)}} {
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO platform.source_subscription(source, remote_id, events, expires_at)
VALUES ('clio', $1, '{matter,contact}', $2) RETURNING id::text`, "r-"+s.key, s.expires).Scan(&id); err != nil {
				return err
			}
			ids[s.key] = id
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := NewPGSubscriptions(pool)

	got, err := s.Expiring(ctx, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("Expiring: %v", err)
	}
	if len(got) != 1 || got[0].ID != ids["soon"] || got[0].RemoteID != "r-soon" || len(got[0].Events) != 2 || got[0].LastRenewedAt != nil {
		t.Fatalf("expiring = %+v", got)
	}

	const secret = "s3cr3t-token-value"
	if err := s.MarkFailed(ctx, ids["soon"], "PATCH failed: Authorization: Bearer "+secret); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	got, err = s.Expiring(ctx, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("Expiring: %v", err)
	}
	if len(got) != 1 || got[0].LastError == "" || strings.Contains(got[0].LastError, secret) || !got[0].ExpiresAt.Equal(base) {
		t.Fatalf("after MarkFailed = %+v", got)
	}

	newExp, at := base.Add(24*24*time.Hour), base.Add(-6*24*time.Hour)
	if err := s.MarkRenewed(ctx, ids["soon"], newExp, at); err != nil {
		t.Fatalf("MarkRenewed: %v", err)
	}
	got, err = s.Expiring(ctx, newExp)
	if err != nil {
		t.Fatalf("Expiring: %v", err)
	}
	if len(got) != 1 || got[0].ID != ids["soon"] || got[0].LastError != "" || got[0].LastRenewedAt == nil || !got[0].LastRenewedAt.Equal(at) {
		t.Fatalf("after MarkRenewed = %+v", got)
	}

	tests := []struct {
		name string
		id   string
	}{
		{"unknown id", "00000000-0000-7000-8000-000000000099"},
		{"malformed id", "not-a-uuid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.MarkFailed(ctx, tt.id, "x"); !errors.Is(err, ErrSubscriptionNotFound) {
				t.Fatalf("MarkFailed err = %v", err)
			}
			if err := s.MarkRenewed(ctx, tt.id, newExp, at); !errors.Is(err, ErrSubscriptionNotFound) {
				t.Fatalf("MarkRenewed err = %v", err)
			}
		})
	}
}
