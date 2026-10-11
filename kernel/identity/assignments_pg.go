package identity

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

var _ RoleAssignments = PGAssignments{}

// PGAssignments is the RoleAssignments over core.app_user and core.role: a member holds
// the one role core.app_user.role_id names, by its key. Only an active, unarchived user
// holds a role, and only an active, unarchived role is held. Anyone else holds none, so
// every capability check on them fails closed.
type PGAssignments struct {
	DB db.DB
}

var userIDRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const sqlRolesOf = `SELECT r.key FROM core.app_user u JOIN core.role r ON r.id = u.role_id
WHERE u.id = $1::uuid AND u.status = 'active' AND u.archived_at IS NULL
  AND r.status = 'active' AND r.archived_at IS NULL`

// RolesOf implements RoleAssignments. A user id that is not a uuid holds no role.
func (p PGAssignments) RolesOf(ctx context.Context, u UserID) ([]string, error) {
	if p.DB == nil {
		return nil, fmt.Errorf("roles of user: no database: %w", ErrForbidden)
	}
	if !userIDRE.MatchString(string(u)) {
		return nil, nil
	}
	var key string
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlRolesOf, string(u)).Scan(&key)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("roles of user: %w", err)
	}
	return []string{key}, nil
}

var _ RoleGrants = PGAssignments{}

const sqlGrantsOf = `SELECT g.capability_id, g.level FROM core.role_grant g JOIN core.role r ON r.id = g.role_id
WHERE r.key = $1 AND r.status = 'active' AND r.archived_at IS NULL`

// GrantsOf implements RoleGrants: an active, unarchived role's grants from core.role_grant,
// in the spec's shape (capability id -> level name). Any other role has none.
func (p PGAssignments) GrantsOf(ctx context.Context, role string) (map[string]string, error) {
	if p.DB == nil {
		return nil, fmt.Errorf("grants of role: no database: %w", ErrForbidden)
	}
	out := map[string]string{}
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlGrantsOf, role)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c, l string
			if err := rows.Scan(&c, &l); err != nil {
				return err
			}
			out[c] = l
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("grants of role: %w", err)
	}
	return out, nil
}
