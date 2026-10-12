package matter

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// KindMatterVisibility is the kernel-owned GA4 gate kind; the matter module supplies its effect
// and the kernel registers it (KERNEL-CORE).
const KindMatterVisibility = "core.matter_visibility"

const (
	// GA4: the matter's visibility is set to the approved level.
	sqlMatterVisibility = `UPDATE matter.matter SET visibility = $2,
  updated_by = CASE WHEN visibility <> $2 THEN $3::uuid ELSE updated_by END,
  updated_at = CASE WHEN visibility <> $2 THEN $4 ELSE updated_at END,
  row_version = row_version + CASE WHEN visibility <> $2 THEN 1 ELSE 0 END
WHERE id = $1::uuid AND archived_at IS NULL`
	// GA4 with named team: members not named leave the matter team...
	sqlTeamPrune = `DELETE FROM matter.matter_team WHERE matter_id = $1::uuid AND NOT (user_id = ANY ($2::uuid[]))`
	// ...and every named member is on it, once.
	sqlTeamAdd = `INSERT INTO matter.matter_team (matter_id, user_id, gate_id, added_at, added_by)
SELECT $1::uuid, u, $3::uuid, $4, $5::uuid FROM unnest($2::uuid[]) u
ON CONFLICT (matter_id, user_id) DO NOTHING`
)

// visibilityColumn maps the GA4 payload level to matter.matter.visibility.
var visibilityColumn = map[string]string{"firm": "firm_open", "team": "team_only", "restricted": "restricted"}

// MatterVisibilityEffect is the core.matter_visibility (GA4) effect: the matter's visibility
// becomes the approved level, changed by the approver at now().
func MatterVisibilityEffect(now func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return matterVisibility(ctx, tx, g, now)
	}
}

func matterVisibility(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p struct {
		MatterID    string   `json:"matter_id"`
		Visibility  string   `json:"visibility"`
		TeamUserIDs []string `json:"team_user_ids"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	col, ok := visibilityColumn[p.Visibility]
	if p.MatterID == "" || !ok {
		return fmt.Errorf("approve %s (gate %s): matter and visibility: %w", g.Kind, g.GateID, gates.ErrNotApprovable)
	}
	// rule: ARC-I-042
	// rule: ARC-I-012
	if err := gates.ExecOne(ctx, tx, g, sqlMatterVisibility, p.MatterID, col, by, now()); err != nil {
		return err
	}
	if len(p.TeamUserIDs) == 0 {
		return nil
	}
	// rule: ARC-I-042
	// The approved team becomes exactly the matter's team; a re-run changes nothing.
	if _, err := tx.Exec(ctx, sqlTeamPrune, p.MatterID, p.TeamUserIDs); err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	if _, err := tx.Exec(ctx, sqlTeamAdd, p.MatterID, p.TeamUserIDs, g.GateID, now(), by); err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	return nil
}
