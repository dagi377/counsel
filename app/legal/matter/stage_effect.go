package matter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateStageMove is the matter.stage_move gate (G-ST): an agent-proposed stage advance that a
// person with matters.edit approves.
const GateStageMove = "matter.stage_move"

// ErrStageMoveApprover means a matter.stage_move gate reached its effect without a human
// approver other than its preparer.
var ErrStageMoveApprover = errors.New("matter: stage move needs a human approver other than the preparer")

// stageMovePayload is a matter.stage_move gate payload. ToStage is a stage id or a stage name
// in the matter's practice area.
type stageMovePayload struct {
	MatterID string `json:"matter_id"`
	ToStage  string `json:"to_stage"`
}

// stageListRow is one active stage as stageStateSQL aggregates it.
type stageListRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Ordinal int    `json:"ordinal"`
}

// stageStateSQL locks the matter and returns its current stage, its practice area and that
// area's active stages in ordinal order, as JSON.
const stageStateSQL = `SELECT COALESCE(m.stage_id::text, ''), m.practice_area_id::text,
  COALESCE((SELECT json_agg(json_build_object('id', s.id::text, 'name', s.name, 'ordinal', s.ordinal) ORDER BY s.ordinal)
              FROM matter.stage s
             WHERE s.practice_area_id = m.practice_area_id AND s.archived_at IS NULL)::text, '[]')
  FROM matter.matter m
 WHERE m.id = $1::uuid AND m.archived_at IS NULL
   FOR UPDATE OF m`

// StageMoveEffect is the matter.stage_move gate effect. Inside the gate's tx it checks the
// move against the matter's stage list (StageList.Move), records the stage change with the
// approver as approved_by at clk's instant, and publishes EventStageChanged naming the gate.
// A gate with no human approver other than its preparer, or a move the list refuses, fails
// with nothing written.
func (s *Store) StageMoveEffect(clk clock.Clock) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		var p stageMovePayload
		if err := json.Unmarshal(g.Payload, &p); err != nil || p.MatterID == "" || p.ToStage == "" {
			return fmt.Errorf("matter.stage_move %s: no matter or stage: %w", g.GateID, ErrInvalid)
		}
		if s.Events == nil || clk == nil {
			return fmt.Errorf("matter.stage_move %s: no event emitter or clock: %w", g.GateID, ErrInvalid)
		}
		// rule: ARC-I-016
		// rule: ARC-I-017
		// The agent never approves, and the preparer (or the user an agent run acted for)
		// never approves the move they proposed.
		by, err := humanApprover(g, ErrStageMoveApprover)
		if err != nil {
			return fmt.Errorf("matter.stage_move %s: %w", g.GateID, err)
		}
		if err := s.moveStageTx(ctx, tx, p, by, g.GateID, clk); err != nil {
			return fmt.Errorf("matter.stage_move %s: %w", g.GateID, err)
		}
		return nil
	}
}

// moveStageTx loads the matter's stage state in tx, checks the move and applies it.
func (s *Store) moveStageTx(ctx context.Context, tx pgx.Tx, p stageMovePayload, by, gateID string, clk clock.Clock) error {
	var from, area, list string
	err := tx.QueryRow(ctx, stageStateSQL, p.MatterID).Scan(&from, &area, &list)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var rows []stageListRow
	if err := json.Unmarshal([]byte(list), &rows); err != nil {
		return fmt.Errorf("stage list: %w", err)
	}
	all := make([]Stage, 0, len(rows))
	to := p.ToStage
	for _, r := range rows {
		all = append(all, Stage{ID: r.ID, PracticeAreaID: area, Name: r.Name, Ordinal: r.Ordinal})
		if r.Name == p.ToStage {
			to = r.ID
		}
	}
	c, err := NewStageList(area, all).Move(ID(p.MatterID), from, to, by, clk.Now())
	if err != nil {
		return err
	}
	return s.changeStageTx(ctx, tx, c, gateID)
}
