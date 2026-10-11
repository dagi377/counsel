//go:build integration

package conflict

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

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/migrations"
)

// runsTestDB creates a migrated database on COUNSEL_TEST_DB and returns a pool on it.
func runsTestDB(t *testing.T) *db.Pool {
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
	name := "conflict_runs_" + hex.EncodeToString(b)
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

// Every PGRuns statement parses and runs against the migrated schema: an unknown check is
// ErrNotFound, an unknown or malformed run id is ErrNotFound, and a matter with no runs has none.
func TestPGRunsIntegration(t *testing.T) {
	pool := runsTestDB(t)
	ctx := context.Background()
	s := NewPGRuns(pool)
	const missing = "00000000-0000-7000-8000-000000000099"
	r := Run{CheckID: missing, MatterID: missing, Terms: []string{"t"}, PreparedBy: missing, IndexVersion: 1, Complete: true,
		RanAt: time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC)}
	tests := []struct {
		name    string
		run     func() error
		wantErr error
	}{
		{"append on unknown check", func() error { _, err := s.Append(ctx, r); return err }, ErrNotFound},
		{"get unknown run", func() error { _, err := s.Get(ctx, missing); return err }, ErrNotFound},
		{"get malformed run id", func() error { _, err := s.Get(ctx, "not-a-uuid"); return err }, ErrNotFound},
		{"runs of a matter with none", func() error {
			runs, err := s.Runs(ctx, missing)
			if err == nil && len(runs) != 0 {
				return errors.New("unexpected runs")
			}
			return err
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
