// Package migrationstest helps integration tests build their own databases on one shared
// Postgres server.
package migrationstest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// rolesLock is the advisory lock key every test package takes while it builds a schema.
const rolesLock int64 = 0x636f756e73656c // "counsel"

// LockClusterRoles serialises schema setup across test packages. 0001_init.sql creates the
// cluster-wide roles (counsel_app, counsel_jobs, ...) with IF NOT EXISTS checks, which race when
// `go test ./...` builds several package databases at once ("duplicate key ... pg_authid").
// The frozen 0001 names its roles, so they cannot differ per package; instead every package
// holds this lock on its admin connection (all packages share the admin database, so the lock
// is shared too) from CREATE DATABASE until its schema is applied. Call the returned func, or
// defer it, once the schema is in place.
func LockClusterRoles(ctx context.Context, t testing.TB, admin *pgx.Conn) (unlock func()) {
	t.Helper()
	if _, err := admin.Exec(ctx, "SELECT pg_advisory_lock($1)", rolesLock); err != nil {
		t.Fatalf("lock cluster roles: %v", err)
	}
	return func() {
		if _, err := admin.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", rolesLock); err != nil {
			t.Errorf("unlock cluster roles: %v", err)
		}
	}
}
