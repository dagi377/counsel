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
	// G-PERM: one approved capability grant, recorded under the approving gate. Never to an
	// offboarded user, never to the approver themself (core.temp_grant checks created_by <>
	// user_id), and approval rights on a licensee-only capability only to a licensee. The
	// same gate again finds its grant and inserts nothing.
	sqlGrant = `INSERT INTO core.temp_grant (user_id, capability_id, level, matter_id, reason, approval_id, starts_at, expires_at, created_by)
SELECT u.id, c.id, $3, $4::uuid, $5, $6::uuid, $7, $8, $9::uuid
FROM core.app_user u JOIN core.capability c ON c.id = $2
WHERE u.id = $1::uuid AND u.status <> 'offboarded'
  AND ($3 <> 'approve' OR NOT c.licensee_required_for_approve OR u.licence_type IN ('lawyer','paralegal'))
  AND NOT EXISTS (SELECT 1 FROM core.temp_grant g WHERE g.approval_id = $6::uuid AND g.user_id = u.id
    AND g.capability_id = c.id AND g.matter_id IS NOT DISTINCT FROM $4::uuid)`
	// A re-run finds the grant this gate already made.
	sqlGranted = `UPDATE core.temp_grant SET approval_id = approval_id
WHERE approval_id = $1::uuid AND user_id = $2::uuid AND capability_id = $3 AND matter_id IS NOT DISTINCT FROM $4::uuid`
)

// grantLevels are the levels a G-PERM grant may carry (core.temp_grant.level).
var grantLevels = map[string]bool{"view": true, "own": true, "all": true, "approve": true}

// grant is one approved capability grant.
type grant struct {
	CapabilityID string    `json:"capability_id"`
	Level        string    `json:"level"`
	MatterID     string    `json:"matter_id,omitempty"`
	Reason       string    `json:"reason"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// accessChange is the change G-PERM applies: the user's role, their capability grants, or
// both. The full approved set applies in the gate's tx, or none of it does.
type accessChange struct {
	RoleID string  `json:"role_id,omitempty"`
	Grants []grant `json:"grants,omitempty"`
}

// valid reports whether c is a complete change that is still in force at now.
func (c accessChange) valid(now time.Time) bool {
	if c.RoleID == "" && len(c.Grants) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, g := range c.Grants {
		key := g.CapabilityID + "|" + g.MatterID
		if g.CapabilityID == "" || !grantLevels[g.Level] || g.Reason == "" || !g.ExpiresAt.After(now) || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

// AccessChangeEffect is the platform.access_change (G-PERM) effect: the full approved grant
// set applies at now(): the user's role becomes the approved one and each approved capability
// grant is recorded under the gate.
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
	at := now()
	if p.UserID == "" || len(p.Change) == 0 || dec.Decode(&c) != nil || !c.valid(at) {
		return fmt.Errorf("approve %s (gate %s): change: %w", g.Kind, g.GateID, gates.ErrNotApprovable)
	}
	// rule: ARC-I-019
	// Nobody approves access for themself.
	if by == p.UserID {
		return fmt.Errorf("approve %s (gate %s): own access: %w", g.Kind, g.GateID, gates.ErrNotApprovable)
	}
	// rule: ARC-I-012
	if c.RoleID != "" {
		if err := changeRole(ctx, tx, g, p.UserID, c.RoleID, by, at); err != nil {
			return err
		}
	}
	for _, gr := range c.Grants {
		if err := addGrant(ctx, tx, g, p.UserID, gr, by, at); err != nil {
			return err
		}
	}
	return nil
}

// changeRole moves the user to roleID, or finds them already on it.
func changeRole(ctx context.Context, tx gates.Execer, g registry.Approved, userID, roleID, by string, at time.Time) error {
	tag, err := tx.Exec(ctx, sqlChangeRole, userID, roleID, by, at)
	if err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	if tag.RowsAffected() == 0 {
		return gates.ExecOne(ctx, tx, g, sqlRoleChanged, userID, roleID)
	}
	return nil
}

// addGrant records gr under the gate, or finds the grant the gate already made.
func addGrant(ctx context.Context, tx gates.Execer, g registry.Approved, userID string, gr grant, by string, at time.Time) error {
	matter := nilIfEmpty(gr.MatterID)
	tag, err := tx.Exec(ctx, sqlGrant, userID, gr.CapabilityID, gr.Level, matter, gr.Reason, g.GateID, at, gr.ExpiresAt, by)
	if err != nil {
		return fmt.Errorf("approve %s (gate %s): grant %s: %w", g.Kind, g.GateID, gr.CapabilityID, err)
	}
	if tag.RowsAffected() == 0 {
		return gates.ExecOne(ctx, tx, g, sqlGranted, g.GateID, userID, gr.CapabilityID, matter)
	}
	return nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
