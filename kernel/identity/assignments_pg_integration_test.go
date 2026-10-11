//go:build integration

package identity_test

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/identity"
)

func TestPGAssignmentsOnPostgres(t *testing.T) {
	pool := breakGlassDB(t)
	ctx := context.Background()
	ids := map[string]string{}
	err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var active, pending string
		if err := tx.QueryRow(ctx, `INSERT INTO core.role(key, name, status) VALUES ('member', 'Member', 'active') RETURNING id::text`).Scan(&active); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO core.role(key, name, status) VALUES ('draft', 'Draft', 'pending') RETURNING id::text`).Scan(&pending); err != nil {
			return err
		}
		for _, u := range []struct{ name, role, status string }{
			{"active", active, "active"}, {"suspended", active, "suspended"}, {"pending_role", pending, "active"},
		} {
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO core.app_user(email, display_name, role_id, status)
VALUES ($1 || '@example.test', $1, $2::uuid, $3) RETURNING id::text`, u.name, u.role, u.status).Scan(&id); err != nil {
				return err
			}
			ids[u.name] = id
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := identity.PGAssignments{DB: pool}
	tests := []struct {
		name string
		user identity.UserID
		want []string
	}{
		{"active user, active role", identity.UserID(ids["active"]), []string{"member"}},
		{"suspended user", identity.UserID(ids["suspended"]), nil},
		{"pending role", identity.UserID(ids["pending_role"]), nil},
		{"unknown user", "00000000-0000-0000-0000-0000000000ff", nil},
		{"not a uuid", "nope", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.RolesOf(ctx, tt.user)
			if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("RolesOf = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

// TestPGGrantsOfOnPostgres: a custom role's grants load in the spec's shape; a pending or
// unknown role has none (fail closed).
func TestPGGrantsOfOnPostgres(t *testing.T) {
	pool := breakGlassDB(t)
	ctx := context.Background()
	err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO core.capability(id, name, area) VALUES ('thing.edit', 'Edit things', 'thing') ON CONFLICT DO NOTHING`); err != nil {
			return err
		}
		for _, r := range []struct{ key, status string }{{"senior_clerk", "active"}, {"draft_clerk", "pending"}} {
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO core.role(key, name, status) VALUES ($1, $1, $2) RETURNING id::text`, r.key, r.status).Scan(&id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO core.role_grant(role_id, capability_id, level) VALUES ($1::uuid, 'thing.edit', 'own')`, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	a := identity.PGAssignments{DB: pool}
	tests := []struct {
		role string
		want string
	}{
		{"senior_clerk", "own"},
		{"draft_clerk", ""},
		{"nobody", ""},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			got, err := a.GrantsOf(ctx, tt.role)
			if err != nil || got["thing.edit"] != tt.want {
				t.Fatalf("GrantsOf = %v, %v; want thing.edit=%q", got, err, tt.want)
			}
		})
	}
}
