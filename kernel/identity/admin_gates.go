package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

// ErrInvalidCoreEffect means a WithCoreEffect option is not usable: its kind is not a core.*
// kind, is one of the kernel's own admin kinds, is given twice, or has no effect.
var ErrInvalidCoreEffect = errors.New("identity: invalid core effect")

// AdminGates proposes role changes and user activations as gates and applies them in the
// gates' effects. Build it with NewAdminGates to add app-supplied effects for core.* kinds.
type AdminGates struct {
	Gates GateOpener
	Store AdminStore

	core map[string]registry.EffectFunc // app-supplied effects for core.* kinds
}

// AdminOption configures NewAdminGates.
type AdminOption func(*adminConfig)

type adminConfig struct {
	core map[string]registry.EffectFunc
	errs []error
}

// WithCoreEffect supplies the effect handler for a core.* gate kind the kernel owns but whose
// effect lives in an app. The kernel still owns the kind; the app supplies only the handler.
func WithCoreEffect(kind string, effect registry.EffectFunc) AdminOption {
	return func(c *adminConfig) {
		switch {
		case !strings.HasPrefix(kind, "core.") || len(kind) == len("core."):
			c.errs = append(c.errs, fmt.Errorf("core effect %q: not a core kind: %w", kind, ErrInvalidCoreEffect))
		case kind == KindRoleChange || kind == KindUserActivation:
			c.errs = append(c.errs, fmt.Errorf("core effect %q: kernel admin kind: %w", kind, ErrInvalidCoreEffect))
		case effect == nil:
			c.errs = append(c.errs, fmt.Errorf("core effect %q: nil effect: %w", kind, ErrInvalidCoreEffect))
		case c.core[kind] != nil:
			c.errs = append(c.errs, fmt.Errorf("core effect %q: given twice: %w", kind, ErrInvalidCoreEffect))
		default:
			c.core[kind] = effect
		}
	}
}

// NewAdminGates returns AdminGates on gates and store with the options applied. It fails
// with ErrInvalidCoreEffect when any option is not usable.
func NewAdminGates(gates GateOpener, store AdminStore, opts ...AdminOption) (AdminGates, error) {
	c := adminConfig{core: map[string]registry.EffectFunc{}}
	for _, o := range opts {
		o(&c)
	}
	if err := errors.Join(c.errs...); err != nil {
		return AdminGates{}, fmt.Errorf("admin gates: %w", err)
	}
	return AdminGates{Gates: gates, Store: store, core: c.core}, nil
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
// app to pass when it registers the kinds. It includes the core.* effects supplied through
// WithCoreEffect.
func (a AdminGates) Effects() map[string]registry.EffectFunc {
	out := map[string]registry.EffectFunc{
		KindRoleChange:     a.applyRoleChange,
		KindUserActivation: a.activateUser,
	}
	for k, fn := range a.core {
		out[k] = fn
	}
	return out
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
