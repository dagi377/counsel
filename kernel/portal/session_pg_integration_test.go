//go:build integration

package portal_test

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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/portal"
	"github.com/dagi377/counsel/migrations"
	"github.com/dagi377/counsel/migrations/migrationstest"
)

// portalPGDB creates a fresh database on COUNSEL_TEST_DB with every migration applied and
// returns a pool on it.
func portalPGDB(t *testing.T) *db.Pool {
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
	name := "portal_session_" + hex.EncodeToString(b)
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

func TestPGSessionsOnPostgres(t *testing.T) {
	pool := portalPGDB(t)
	ctx := context.Background()
	store := portal.PGSessions{DB: pool}
	now := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	unknown := strings.Repeat("ab", 32)

	if _, err := store.TakeToken(ctx, unknown, now); !errors.Is(err, portal.ErrBadToken) {
		t.Fatalf("unknown token err = %v, want ErrBadToken", err)
	}
	if _, _, err := store.Session(ctx, unknown, now); !errors.Is(err, portal.ErrNoSession) {
		t.Fatalf("unknown session err = %v, want ErrNoSession", err)
	}
	if err := store.EndSession(ctx, unknown, now); err != nil {
		t.Fatalf("end unknown session: %v", err)
	}

	// The table refuses anything that is not a sha256 hex key, so a raw token cannot be stored.
	err := store.PutToken(ctx, "raw-token-value", portal.Guest{ID: "00000000-0000-0000-0000-000000000001"}, now.Add(time.Minute))
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("raw key insert err = %v, want check violation 23514", err)
	}
}
