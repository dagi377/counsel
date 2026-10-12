package access

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

// KindOffboarding is the kernel-owned GA12 gate kind whose effect the access module supplies;
// the kernel registers it (KERNEL-CORE). core.matter_visibility is the matter module's
// (ARC-I-041: only matter writes matter tables).
const KindOffboarding = "core.offboarding"

const (
	// GA12: the leaver is offboarded. Only a user not yet offboarded moves.
	sqlOffboard = `UPDATE core.app_user SET status = 'offboarded', offboarded_at = $3,
  updated_by = $2::uuid, updated_at = $3, row_version = row_version + 1
WHERE id = $1::uuid AND status <> 'offboarded'`
	// A re-run finds the user already offboarded and changes nothing.
	sqlOffboarded = `UPDATE core.app_user SET status = status WHERE id = $1::uuid AND status = 'offboarded'`
)

// UserEvents publishes user.deactivated when an offboarding applies (identity.UserEvents).
type UserEvents interface {
	Deactivated(ctx context.Context, tx pgx.Tx, b identity.Deactivated) error
}

// OffboardingEffect is the core.offboarding (GA12) effect: the leaver is offboarded at now()
// and user.deactivated is published in the gate's tx. A re-run finds the user offboarded,
// changes nothing and publishes nothing.
func OffboardingEffect(now func() time.Time, events UserEvents) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return offboard(ctx, tx, g, now, func(ctx context.Context, b identity.Deactivated) error {
			if events == nil {
				return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, identity.ErrNoEmitter)
			}
			return events.Deactivated(ctx, tx, b)
		})
	}
}

func offboard(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time, publish func(context.Context, identity.Deactivated) error) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p struct {
		UserID string `json:"user_id"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.UserID == "" {
		return fmt.Errorf("approve %s (gate %s): no user: %w", g.Kind, g.GateID, gates.ErrNotApprovable)
	}
	// rule: ARC-I-012
	// user.active is gate-owned: the leaver is offboarded only here.
	tag, err := tx.Exec(ctx, sqlOffboard, p.UserID, by, now())
	if err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	if tag.RowsAffected() == 0 {
		return gates.ExecOne(ctx, tx, g, sqlOffboarded, p.UserID)
	}
	return publish(ctx, identity.Deactivated{UserID: p.UserID, GateID: g.GateID})
}
