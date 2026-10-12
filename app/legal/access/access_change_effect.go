package access

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// KindAccessChange is the G-PERM kind whose effect the access module supplies.
const KindAccessChange = "platform.access_change"

const (
	// G-PERM: the user takes the approved role. Only an active role moves a user, a
	// licensee-only role only a licensee, and an offboarded user never moves.
	sqlChangeRole = `UPDATE core.app_user u SET role_id = $2::uuid, updated_by = $3::uuid, updated_at = $4,
  row_version = u.row_version + 1
WHERE u.id = $1::uuid AND u.status <> 'offboarded' AND u.role_id <> $2::uuid
  AND EXISTS (SELECT 1 FROM core.role r WHERE r.id = $2::uuid AND r.status = 'active'
    AND (NOT r.licensee_only OR u.licence_type IN ('lawyer','paralegal')))`
	// A re-run finds the user on the role already and changes nothing.
	sqlRoleChanged = `UPDATE core.app_user SET role_id = role_id WHERE id = $1::uuid AND role_id = $2::uuid`
)

// accessChange is the only change shape G-PERM applies: the user's role.
type accessChange struct {
	RoleID string `json:"role_id"`
}

// AccessChangeEffect is the platform.access_change (G-PERM) effect: the user's role becomes
// the approved one at now().
func AccessChangeEffect(now func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return changeAccess(ctx, tx, g, now)
	}
}

func changeAccess(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p struct {
		UserID string          `json:"user_id"`
		Change json.RawMessage `json:"change"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	var c accessChange
	dec := json.NewDecoder(bytes.NewReader(p.Change))
	dec.DisallowUnknownFields()
	if p.UserID == "" || len(p.Change) == 0 || dec.Decode(&c) != nil || c.RoleID == "" {
		return fmt.Errorf("approve %s (gate %s): change: %w", g.Kind, g.GateID, gates.ErrNotApprovable)
	}
	// rule: ARC-I-012
	tag, err := tx.Exec(ctx, sqlChangeRole, p.UserID, c.RoleID, by, now())
	if err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	if tag.RowsAffected() == 0 {
		return gates.ExecOne(ctx, tx, g, sqlRoleChanged, p.UserID, c.RoleID)
	}
	return nil
}
