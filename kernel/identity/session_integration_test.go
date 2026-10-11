//go:build integration

package identity

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/registry"
)

// readVars returns the session variables row-level security reads in the current transaction.
func readVars(ctx context.Context, tx pgx.Tx) (map[string]string, error) {
	out := map[string]string{}
	for _, k := range []string{db.VarUserID, db.VarPrincipalKind, db.VarRunID, db.VarActingFor} {
		var v string
		if err := tx.QueryRow(ctx, "SELECT coalesce(current_setting($1, true), '')", k).Scan(&v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

// TestTxSessionIsPerRequest binds a principal in one request and checks a second request on
// the same pooled connection sees the session variables empty.
func TestTxSessionIsPerRequest(t *testing.T) {
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cfg.MaxConns = 1 // every transaction reuses the same pooled connection
	pool, err := db.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer pool.Close()
	s := NewTxSession(pool)

	tests := []struct {
		name string
		p    Principal
		want map[string]string
	}{
		{"user", Principal{Kind: registry.PrincipalUser, UserID: "u1"},
			map[string]string{db.VarUserID: "u1", db.VarPrincipalKind: "user", db.VarRunID: "", db.VarActingFor: ""}},
		{"agent", Principal{Kind: registry.PrincipalAgent, RunID: "r1", ActingFor: "u2", Grant: []string{"x.read"}},
			map[string]string{db.VarUserID: "u2", db.VarPrincipalKind: "agent", db.VarRunID: "r1", db.VarActingFor: "u2"}},
	}
	empty := map[string]string{db.VarUserID: "", db.VarPrincipalKind: "", db.VarRunID: "", db.VarActingFor: ""}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var first, second map[string]string
			if err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				if err := s.Bind(ctx, tx, tc.p); err != nil {
					return err
				}
				first, err = readVars(ctx, tx)
				return err
			}); err != nil {
				t.Fatalf("first request: %v", err)
			}
			if err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				second, err = readVars(ctx, tx)
				return err
			}); err != nil {
				t.Fatalf("second request: %v", err)
			}
			for k, v := range tc.want {
				if first[k] != v {
					t.Errorf("first %s = %q, want %q", k, first[k], v)
				}
				if second[k] != empty[k] {
					t.Errorf("second %s = %q, want empty", k, second[k])
				}
			}
		})
	}
}

// TestTxSessionRejectsMissingPrincipal checks Bind refuses a principal it cannot identify.
func TestTxSessionRejectsMissingPrincipal(t *testing.T) {
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pool, err := db.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer pool.Close()
	s := NewTxSession(pool)
	tests := []struct {
		name string
		p    Principal
	}{
		{"zero", Principal{}},
		{"user without id", Principal{Kind: registry.PrincipalUser}},
		{"agent without run", Principal{Kind: registry.PrincipalAgent, ActingFor: "u1"}},
		{"agent without acting user", Principal{Kind: registry.PrincipalAgent, RunID: "r1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error { return s.Bind(ctx, tx, tc.p) })
			if !errors.Is(err, ErrNoPrincipal) {
				t.Fatalf("err = %v, want ErrNoPrincipal", err)
			}
		})
	}
}
