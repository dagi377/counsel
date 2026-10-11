//go:build integration

package boot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/migrations"
)

// reportAuthDB creates a fresh database on COUNSEL_TEST_DB with every migration applied.
func reportAuthDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "boot_report_auth_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if err := migrations.Apply(ctx, conn.Conn(), ""); err != nil {
		t.Fatal(err)
	}
	return pool
}

// userRoles reads a user's role key from core.app_user, standing in for the identity
// store's RoleAssignments. An unknown user holds no roles.
type userRoles struct{ pool *pgxpool.Pool }

func (r userRoles) RolesOf(ctx context.Context, u identity.UserID) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
SELECT ro.key FROM core.app_user au JOIN core.role ro ON ro.id = au.role_id
 WHERE au.id::text = $1 AND au.status = 'active'`, string(u))
	if err != nil {
		return nil, fmt.Errorf("roles of: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// The report authorizer over the real identity.Checker and stored role assignments.
func TestReportAuthOverStoredRoles(t *testing.T) {
	pool := reportAuthDB(t)
	ctx := context.Background()
	var role, viewer, other string
	if err := pool.QueryRow(ctx, `INSERT INTO core.role(key, name, status) VALUES ('viewer', 'Viewer', 'active') RETURNING id::text`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	for _, u := range []struct {
		email string
		into  *string
	}{{"viewer@example.com", &viewer}, {"other@example.com", &other}} {
		if err := pool.QueryRow(ctx, `
INSERT INTO core.app_user(email, display_name, role_id, status) VALUES ($1, $1, $2, 'active') RETURNING id::text`,
			u.email, role).Scan(u.into); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE core.app_user SET status = 'suspended' WHERE id = $1`, other); err != nil {
		t.Fatal(err)
	}
	roles := identity.NewRoles()
	roles.Capability(registry.Capability{ID: capRun})
	roles.Capability(registry.Capability{ID: capOther})
	roles.Role(registry.Role{ID: "viewer", Capabilities: []string{capRun}})
	auth := newReportAuth(identity.NewChecker(roles, userRoles{pool}), nil)

	tests := []struct {
		name string
		user string
		cap  string
		want bool
	}{
		{"active user with the capability", viewer, capRun, true},
		{"active user without the capability", viewer, capOther, false},
		{"suspended user", other, capRun, false},
		{"unknown user", "0190d0f4-0000-7000-8000-0000000000ff", capRun, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := auth.Can(ctx, tt.user, tt.cap)
			if err != nil && got {
				t.Fatal("allowed on error")
			}
			if got != tt.want {
				t.Fatalf("Can = %v (err %v), want %v", got, err, tt.want)
			}
		})
	}
}
