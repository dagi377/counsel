package matter

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// KindWallLift is the kernel-owned GA5 gate kind; the matter module supplies its effect.
const KindWallLift = "core.wall_lift"

const (
	// GA5: the audited lift record, written once per gate, only for a wall that is not lifted
	// and only when no approver is screened by it.
	sqlWallLiftRecord = `INSERT INTO matter.ethical_wall_lift (gate_id, wall_id, user_id, lifted_by, lifted_at)
SELECT $1::uuid, w.id, NULLIF($3, '')::uuid, $4::uuid, $5 FROM matter.ethical_wall w
 WHERE w.id = $2::uuid AND w.status <> 'lifted'
   AND NOT EXISTS (SELECT 1 FROM matter.ethical_wall_screened_user s
                    WHERE s.wall_id = w.id AND s.user_id = ANY ($6::uuid[]))
ON CONFLICT (gate_id) DO NOTHING`
	sqlWallLifted = `UPDATE matter.ethical_wall_lift SET gate_id = gate_id WHERE gate_id = $1::uuid`
	// GA5 whole wall: the wall is lifted.
	sqlLiftWall = `UPDATE matter.ethical_wall SET status = 'lifted', lifted_at = $2, approval_id = $3::uuid
WHERE id = $1::uuid AND status <> 'lifted'`
	// GA5 one person: the person is no longer screened; the lift record keeps the history.
	sqlUnscreen = `DELETE FROM matter.ethical_wall_screened_user WHERE wall_id = $1::uuid AND user_id = $2::uuid`
	// Offboarding hand-over: the leaver's live matters move to the hand-over user.
	sqlReassignResponsible = `UPDATE matter.matter SET
  responsible_lawyer_id = CASE WHEN responsible_lawyer_id = $1::uuid THEN $2::uuid ELSE responsible_lawyer_id END,
  responsible_staff_id = CASE WHEN responsible_staff_id = $1::uuid THEN $2::uuid ELSE responsible_staff_id END,
  updated_at = $3, row_version = row_version + 1
WHERE (responsible_lawyer_id = $1::uuid OR responsible_staff_id = $1::uuid)
  AND status IN ('pending','open','closing') AND archived_at IS NULL`
)

// WallLiftEffect is the core.wall_lift (GA5) effect: the wall, or one screened person on it,
// is lifted, with an audited lift record naming the approver. An approver screened by the
// wall never lifts it, and the effect needs minApprovers distinct approvers (the spec's).
func WallLiftEffect(now func() time.Time, minApprovers int) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return liftWall(ctx, tx, g, now, minApprovers)
	}
}

func liftWall(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time, minApprovers int) error {
	// rule: ARC-I-016
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	// rule: ARC-I-019
	if err := gates.MinApprovers(g, minApprovers); err != nil {
		return err
	}
	var p struct {
		WallID string `json:"wall_id"`
		UserID string `json:"user_id"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.WallID == "" {
		return gates.Refuse(g, "no wall")
	}
	if p.UserID != "" && p.UserID == by {
		return fmt.Errorf("approve %s (gate %s): the screened person approves: %w", g.Kind, g.GateID, gates.ErrPreparerApproves)
	}
	approvers := make([]string, 0, len(g.Approvers))
	for _, a := range g.Approvers {
		approvers = append(approvers, a.UserID)
	}
	at := now()
	// rule: ARC-I-042
	// The lift is audited first; a wall already lifted by this gate is a re-run.
	recorded, err := gates.Matches(ctx, tx, g, sqlWallLiftRecord, g.GateID, p.WallID, p.UserID, by, at, approvers)
	if err != nil {
		return err
	}
	if !recorded {
		if again, err := gates.Matches(ctx, tx, g, sqlWallLifted, g.GateID); err != nil || again {
			return err
		}
		return gates.Refuse(g, "wall missing, lifted, or an approver is screened by it")
	}
	// rule: ARC-I-012
	if p.UserID == "" {
		return gates.ExecOne(ctx, tx, g, sqlLiftWall, p.WallID, at, g.GateID)
	}
	if _, err := tx.Exec(ctx, sqlUnscreen, p.WallID, p.UserID); err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	return nil
}

// ReassignResponsible moves the leaver's live matters (responsible lawyer or staff) to the
// hand-over user (offboarding, GA12).
func ReassignResponsible(ctx context.Context, tx gates.Execer, from, to string, at time.Time) error {
	if _, err := tx.Exec(ctx, sqlReassignResponsible, from, to, at); err != nil {
		return fmt.Errorf("reassign matters: %w", err)
	}
	return nil
}
