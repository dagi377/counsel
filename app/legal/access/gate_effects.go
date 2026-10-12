package access

import (
	"context"
	"encoding/json"
	"errors"
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
	// GA12: the approved offboarding is scheduled once per gate.
	sqlScheduleOffboarding = `INSERT INTO core.offboarding_schedule (gate_id, user_id, handover_user_id, cutoff_at, approved_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::uuid) ON CONFLICT (gate_id) DO NOTHING`
	// Claims the next offboarding due at $1, marking it applied.
	sqlClaimDueOffboarding = `UPDATE core.offboarding_schedule SET applied_at = $1
WHERE gate_id = (SELECT gate_id FROM core.offboarding_schedule
                  WHERE applied_at IS NULL AND cutoff_at <= $1
                  ORDER BY cutoff_at, gate_id LIMIT 1 FOR UPDATE SKIP LOCKED)
RETURNING gate_id::text, user_id::text, handover_user_id::text, approved_by::text`
	// The leaver is offboarded. Only a user not yet offboarded moves.
	sqlOffboard = `UPDATE core.app_user SET status = 'offboarded', offboarded_at = $3,
  updated_by = $2::uuid, updated_at = $3, row_version = row_version + 1
WHERE id = $1::uuid AND status <> 'offboarded'`
)

// UserEvents publishes user.deactivated when an offboarding applies (identity.UserEvents).
type UserEvents interface {
	Deactivated(ctx context.Context, tx pgx.Tx, b identity.Deactivated) error
}

// Handover moves the leaver's open matters and tasks to the hand-over user in the tx. Boot
// joins the matter and work modules' reassignments (each module writes only its own tables).
type Handover func(ctx context.Context, tx gates.Execer, from, to string, at time.Time) error

// OffboardingEffect is the core.offboarding (GA12) effect: approval schedules the
// offboarding for cutoff_at and does not cut access early. When cutoff_at is now or past,
// every due offboarding applies at once in the gate's tx (ApplyDueOffboardings); otherwise the
// offboarding job applies it at cutoff_at.
func OffboardingEffect(now func() time.Time, events UserEvents, handover Handover) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return scheduleOffboarding(ctx, tx, g, now, func(ctx context.Context) error {
			return ApplyDueOffboardings(ctx, tx, now(), events, handover)
		})
	}
}

type offboardingPayload struct {
	UserID   string            `json:"user_id"`
	Handover []json.RawMessage `json:"handover"`
	CutoffAt string            `json:"cutoff_at"`
}

// handoverUser is the user the leaver's work goes to: the first handover item, a user id or
// an object with "user_id".
func (p offboardingPayload) handoverUser() string {
	if len(p.Handover) == 0 {
		return ""
	}
	var id string
	if json.Unmarshal(p.Handover[0], &id) == nil {
		return id
	}
	var obj struct {
		UserID string `json:"user_id"`
	}
	_ = json.Unmarshal(p.Handover[0], &obj)
	return obj.UserID
}

func scheduleOffboarding(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time, applyDue func(context.Context) error) error {
	var p offboardingPayload
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	// rule: ARC-I-016
	// Neither the preparer, an agent, nor the leaver approves the offboarding.
	by, err := gates.ApproverNot(g, p.UserID)
	if err != nil {
		return err
	}
	to := p.handoverUser()
	cutoff, perr := time.Parse(time.RFC3339, p.CutoffAt)
	if p.UserID == "" || to == "" || to == p.UserID || perr != nil {
		return gates.Refuse(g, "user, hand-over user and cut-off")
	}
	// rule: ARC-I-012
	// user.active is gate-owned: the leaver is offboarded only through this schedule.
	if _, err := tx.Exec(ctx, sqlScheduleOffboarding, g.GateID, p.UserID, to, cutoff, by); err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	if cutoff.After(now()) {
		return nil
	}
	return applyDue(ctx)
}

// ApplyDueOffboardings applies every scheduled offboarding due at now, in tx: the leaver's
// open matters and tasks go to the hand-over user, the leaver is offboarded and
// user.deactivated is published. Each row applies once (it is marked applied as it is claimed).
func ApplyDueOffboardings(ctx context.Context, tx pgx.Tx, now time.Time, events UserEvents, handover Handover) error {
	if events == nil || handover == nil {
		return fmt.Errorf("apply offboardings: %w", identity.ErrNoEmitter)
	}
	return applyDue(ctx, tx, now, handover, func(ctx context.Context, b identity.Deactivated) error {
		return events.Deactivated(ctx, tx, b)
	})
}

func applyDue(ctx context.Context, tx gates.Querier, now time.Time, handover Handover, publish func(context.Context, identity.Deactivated) error) error {
	for {
		var gateID, user, to, by string
		err := tx.QueryRow(ctx, sqlClaimDueOffboarding, now).Scan(&gateID, &user, &to, &by)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("apply offboardings: %w", err)
		}
		if err := handover(ctx, tx, user, to, now); err != nil {
			return fmt.Errorf("apply offboarding (gate %s): %w", gateID, err)
		}
		tag, err := tx.Exec(ctx, sqlOffboard, user, by, now)
		if err != nil {
			return fmt.Errorf("apply offboarding (gate %s): %w", gateID, err)
		}
		if tag.RowsAffected() == 0 {
			continue // already offboarded: nothing to publish
		}
		if err := publish(ctx, identity.Deactivated{UserID: user, GateID: gateID}); err != nil {
			return err
		}
	}
}
