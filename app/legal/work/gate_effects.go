package work

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// Gate effect SQL. Each statement re-matches a row the same approval already moved, so a
// re-run changes nothing.
const (
	// G-AR: the automation rule is enabled.
	sqlEnableAutomation = `UPDATE work.automation_rule SET enabled = true WHERE id = $1::uuid`
	// G-W1: the item moves along its workflow's transition into the done group. An item
	// already in a done status is left as it is.
	sqlOutputDone = `UPDATE work.work_item w
   SET status_id = COALESCE(nxt.to_status_id, w.status_id),
       updated_by = CASE WHEN nxt.to_status_id IS NULL THEN w.updated_by ELSE $2::uuid END,
       updated_at = CASE WHEN nxt.to_status_id IS NULL THEN w.updated_at ELSE $3 END,
       row_version = w.row_version + CASE WHEN nxt.to_status_id IS NULL THEN 0 ELSE 1 END
  FROM work.status cur
  LEFT JOIN LATERAL (
        SELECT t.to_status_id FROM work.transition t
          JOIN work.status s ON s.id = t.to_status_id
         WHERE t.from_status_id = cur.id AND s.status_group = 'done'
         ORDER BY s.name LIMIT 1) nxt ON cur.status_group <> 'done'
 WHERE w.id = $1::uuid AND w.archived_at IS NULL AND cur.id = w.status_id
   AND (cur.status_group = 'done' OR nxt.to_status_id IS NOT NULL)`
)

// AutomationEffect is the work.automation (G-AR) effect: the approved rule is enabled. The
// agent never enables one.
func AutomationEffect() registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return enableAutomation(ctx, tx, g)
	}
}

func enableAutomation(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	if _, err := gates.Approver(g); err != nil {
		return err
	}
	var p struct {
		RuleID string `json:"rule_id"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.RuleID == "" {
		return fmt.Errorf("approve %s (gate %s): no rule: %w", g.Kind, g.GateID, gates.ErrNotApprovable)
	}
	return gates.ExecOne(ctx, tx, g, sqlEnableAutomation, p.RuleID)
}

// OutputEffect is the work.output (G-W1) effect: the approved item moves to Done along its
// workflow, recorded as updated by the approver at now().
func OutputEffect(now func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return outputDone(ctx, tx, g, now)
	}
}

func outputDone(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p struct {
		ItemID string `json:"item_id"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.ItemID == "" {
		return fmt.Errorf("approve %s (gate %s): no item: %w", g.Kind, g.GateID, gates.ErrNotApprovable)
	}
	return gates.ExecOne(ctx, tx, g, sqlOutputDone, p.ItemID, by, now())
}
