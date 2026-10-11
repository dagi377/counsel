package matter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/outbox"
)

// Outbox event types the matter store publishes (spec/eng/a2-interfaces.json).
const (
	EventOpened       = "matter.opened"
	EventStageChanged = "matter.stage_changed"
)

// Opened is the body of EventOpened: ids, the number and the business date only.
type Opened struct {
	MatterID          string `json:"matter_id"`
	Number            string `json:"number"`
	ClientID          string `json:"client_id"`
	PracticeAreaID    string `json:"practice_area_id"`
	ResponsibleUserID string `json:"responsible_user_id"`
	OpenedOn          string `json:"opened_on"`
}

// StageChanged is the body of EventStageChanged. FromStageID is empty for a first stage.
type StageChanged struct {
	MatterID    string `json:"matter_id"`
	FromStageID string `json:"from_stage_id,omitempty"`
	ToStageID   string `json:"to_stage_id"`
	GateID      string `json:"gate_id,omitempty"`
}

const openMatterSQL = `UPDATE matter.matter
   SET status = 'open', open_date = $2::date, updated_by = NULLIF($3, '')::uuid,
       updated_at = now(), row_version = row_version + 1
 WHERE id = $1::uuid AND status = 'pending' AND archived_at IS NULL
RETURNING number, client_id::text, practice_area_id::text, responsible_lawyer_id::text`

// Open moves pending matter id to open on business date on and publishes EventOpened in the
// same tx. The caller has passed Lifecycle.Transition (conflict cleared through G2); the
// matter.opening gate effect is the caller. A matter that is not pending is ErrNotFound.
func (s *Store) Open(ctx context.Context, id ID, on clock.Date, by string) error {
	if strings.TrimSpace(string(id)) == "" {
		return fmt.Errorf("open matter: %w", ErrInvalid)
	}
	if s.Events == nil {
		return fmt.Errorf("open matter: no event emitter: %w", ErrInvalid)
	}
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		ev := Opened{MatterID: string(id), OpenedOn: on.String()}
		err := tx.QueryRow(ctx, openMatterSQL, string(id), on.String(), by).
			Scan(&ev.Number, &ev.ClientID, &ev.PracticeAreaID, &ev.ResponsibleUserID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		// rule: ARC-I-030
		// The event commits with the status change.
		return s.Events.Emit(ctx, tx, outbox.Subject{Kind: matterkind.Subject, ID: string(id)}, EventOpened, ev)
	})
	if err != nil {
		return fmt.Errorf("open matter %s: %w", id, err)
	}
	return nil
}

// changeStageSQL moves the matter only from the stage the change names and appends the
// stage_change row in the same statement.
const changeStageSQL = `WITH moved AS (
  UPDATE matter.matter SET stage_id = $3::uuid, updated_at = now(), row_version = row_version + 1
   WHERE id = $1::uuid AND stage_id IS NOT DISTINCT FROM NULLIF($2, '')::uuid AND archived_at IS NULL
  RETURNING id)
INSERT INTO matter.stage_change (matter_id, from_stage_id, to_stage_id, approved_by, at)
SELECT id, NULLIF($2, '')::uuid, $3::uuid, NULLIF($4, '')::uuid, $5 FROM moved
RETURNING id::text`

// ChangeStage applies c (from StageList.Move) and publishes EventStageChanged in the same tx.
// gateID is the approving gate, or empty. A matter no longer in c.FromStageID is ErrNotFound.
func (s *Store) ChangeStage(ctx context.Context, c StageChange, gateID string) error {
	if c.MatterID == "" || c.ToStageID == "" || c.At.IsZero() {
		return fmt.Errorf("change stage: %w", ErrInvalid)
	}
	if s.Events == nil {
		return fmt.Errorf("change stage: no event emitter: %w", ErrInvalid)
	}
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var changeID string
		err := tx.QueryRow(ctx, changeStageSQL, string(c.MatterID), c.FromStageID, c.ToStageID, c.ApprovedBy, c.At).
			Scan(&changeID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		// rule: ARC-I-030
		body := StageChanged{MatterID: string(c.MatterID), FromStageID: c.FromStageID, ToStageID: c.ToStageID, GateID: gateID}
		return s.Events.Emit(ctx, tx, outbox.Subject{Kind: matterkind.Subject, ID: string(c.MatterID)}, EventStageChanged, body)
	})
	if err != nil {
		return fmt.Errorf("change stage %s: %w", c.MatterID, err)
	}
	return nil
}
