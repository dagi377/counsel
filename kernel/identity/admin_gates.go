package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// Admin gate kinds. A role change or a user activation never applies directly: it opens a
// gate and the change applies only in the gate's effect, once an approver holding the kind's
// capability has approved it.
const (
	// KindRoleChange is the role change gate (spec GA1, access). Approver capability admin.roles.
	KindRoleChange = "core.role_change"
	// KindUserActivation is the user activation gate (spec GA2, access). Approver capability
	// admin.users.
	KindUserActivation = "core.user_activation"
)

// Subject kinds the admin gates are about.
const (
	SubjectRole = "core.role"
	SubjectUser = "core.user"
)

// ErrInvalidAdminChange means a role change or activation payload is missing a field.
var ErrInvalidAdminChange = errors.New("identity: invalid admin change")

// RoleChange is the core.role_change payload: the role and the change to apply to it.
type RoleChange struct {
	RoleID string          `json:"role_id"`
	Change json.RawMessage `json:"change"`
}

// UserActivation is the core.user_activation payload: the user to activate and their role.
type UserActivation struct {
	UserID string `json:"user_id"`
	RoleID string `json:"role_id"`
}

func (c RoleChange) validate() error {
	var obj map[string]json.RawMessage
	if c.RoleID == "" || json.Unmarshal(c.Change, &obj) != nil || obj == nil {
		return ErrInvalidAdminChange
	}
	return nil
}

func (a UserActivation) validate() error {
	if a.UserID == "" || a.RoleID == "" {
		return ErrInvalidAdminChange
	}
	return nil
}

// AdminStore applies approved admin changes. Both run inside the gate effect's tx.
type AdminStore interface {
	ApplyRoleChange(ctx context.Context, tx pgx.Tx, c RoleChange) error
	ActivateUser(ctx context.Context, tx pgx.Tx, a UserActivation) error
}

// GateOpener opens gates. gate.Engine implements it.
type GateOpener interface {
	Open(ctx context.Context, tx pgx.Tx, kind string, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.GateID, error)
}

// AdminGates proposes role changes and user activations as gates and applies them in the
// gates' effects.
type AdminGates struct {
	Gates GateOpener
	Store AdminStore
}

// ProposeRoleChange opens a core.role_change gate for c, prepared by by.
func (a AdminGates) ProposeRoleChange(ctx context.Context, tx pgx.Tx, c RoleChange, by Principal) (gate.GateID, error) {
	if err := c.validate(); err != nil {
		return "", fmt.Errorf("propose role change: %w", err)
	}
	return a.open(ctx, tx, KindRoleChange, gate.Subject{Kind: SubjectRole, ID: c.RoleID}, c, by)
}

// ProposeActivation opens a core.user_activation gate for u, prepared by by.
func (a AdminGates) ProposeActivation(ctx context.Context, tx pgx.Tx, u UserActivation, by Principal) (gate.GateID, error) {
	if err := u.validate(); err != nil {
		return "", fmt.Errorf("propose activation: %w", err)
	}
	return a.open(ctx, tx, KindUserActivation, gate.Subject{Kind: SubjectUser, ID: u.UserID}, u, by)
}

func (a AdminGates) open(ctx context.Context, tx pgx.Tx, kind string, s gate.Subject, payload any, by Principal) (gate.GateID, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("open %s: encode payload: %w", kind, err)
	}
	id, err := a.Gates.Open(ctx, tx, kind, s, body, by)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", kind, err)
	}
	return id, nil
}

// Effects returns the effect handlers for the admin gate kinds, keyed by gate kind, for the
// app to pass when it registers the kinds.
func (a AdminGates) Effects() map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{
		KindRoleChange:     a.applyRoleChange,
		KindUserActivation: a.activateUser,
	}
}

func (a AdminGates) applyRoleChange(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	var c RoleChange
	if err := decodeAdmin(g, KindRoleChange, &c); err != nil {
		return err
	}
	if err := c.validate(); err != nil {
		return fmt.Errorf("apply role change %s: %w", g.GateID, err)
	}
	if err := a.Store.ApplyRoleChange(ctx, tx, c); err != nil {
		return fmt.Errorf("apply role change %s: %w", g.GateID, err)
	}
	return nil
}

func (a AdminGates) activateUser(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	var u UserActivation
	if err := decodeAdmin(g, KindUserActivation, &u); err != nil {
		return err
	}
	if err := u.validate(); err != nil {
		return fmt.Errorf("activate user %s: %w", g.GateID, err)
	}
	if err := a.Store.ActivateUser(ctx, tx, u); err != nil {
		return fmt.Errorf("activate user %s: %w", g.GateID, err)
	}
	return nil
}

func decodeAdmin(g registry.Approved, kind string, v any) error {
	if g.Kind != kind {
		return fmt.Errorf("effect %s: kind %q, want %q: %w", g.GateID, g.Kind, kind, ErrInvalidAdminChange)
	}
	if err := json.Unmarshal(g.Payload, v); err != nil {
		return fmt.Errorf("effect %s: decode payload: %w", g.GateID, ErrInvalidAdminChange)
	}
	return nil
}
