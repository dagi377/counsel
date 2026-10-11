package matter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/registry"
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
		return s.openTx(ctx, tx, id, on, by)
	})
	if err != nil {
		return fmt.Errorf("open matter %s: %w", id, err)
	}
	return nil
}

// openTx is Open inside the caller's tx.
func (s *Store) openTx(ctx context.Context, tx pgx.Tx, id ID, on clock.Date, by string) error {
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
}

// openingPayload is a matter.opening gate payload. PATCH /matters/{matter_id} status=open
// sends matter_id and status "open" for a pending matter; POST /matters sends the create shape
// (matter_id chosen by the route, number, short_name, client_id, ...) for a matter that does
// not exist yet.
type openingPayload struct {
	MatterID            string `json:"matter_id"`
	Status              string `json:"status"`
	Number              string `json:"number"`
	ShortName           string `json:"short_name"`
	ClientID            string `json:"client_id"`
	PracticeAreaID      string `json:"practice_area_id"`
	ResponsibleLawyerID string `json:"responsible_lawyer_id"`
	Visibility          string `json:"visibility"`
}

// createShape reports whether c asks to create the matter as well as open it.
func (c openingPayload) createShape() bool { return c.Status == "" && c.ClientID != "" }

// createOpenSQL inserts a new matter already open on the business date $10, under the id the
// gate payload carries.
const createOpenSQL = `INSERT INTO matter.matter
  (id, number, short_name, client_id, practice_area_id, responsible_lawyer_id, visibility, retention_class,
   status, open_date, created_by, updated_by)
VALUES ($1::uuid, $2, $3, $4::uuid, $5::uuid, $6::uuid, $7, $8, 'open', $10::date, NULLIF($9, '')::uuid, NULLIF($9, '')::uuid)
RETURNING number, client_id::text, practice_area_id::text, responsible_lawyer_id::text`

// ErrOpeningApprover means a matter.opening gate reached its effect without a human approver
// other than its preparer.
var ErrOpeningApprover = errors.New("matter: opening needs a human approver other than the preparer")

// openingApprover returns the approving user of a matter.opening gate g.
func openingApprover(g registry.Approved) (string, error) {
	return humanApprover(g, ErrOpeningApprover)
}

// humanApprover returns the approving user of g. It refuses, with refused, a gate with no
// approver, an agent approver, or an approver who prepared it (directly or through an agent run).
func humanApprover(g registry.Approved, refused error) (string, error) {
	if len(g.Approvers) == 0 {
		return "", refused
	}
	for _, a := range g.Approvers {
		// rule: ARC-I-017
		// The agent never approves.
		if a.Kind != registry.PrincipalUser || a.UserID == "" {
			return "", refused
		}
		// rule: ARC-I-016
		// The preparer never approves, nor the user an agent run prepared it for.
		if a.UserID == g.Preparer.UserID || a.UserID == g.Preparer.ActingFor {
			return "", refused
		}
	}
	return g.Approvers[0].UserID, nil
}

// preparerUser is the user who prepared g: the session user, or the user an agent run acts for.
func preparerUser(g registry.Approved) string {
	if g.Preparer.Kind == registry.PrincipalAgent {
		return g.Preparer.ActingFor
	}
	return g.Preparer.UserID
}

// OpeningEffect is the matter.opening gate effect. A status change to open opens the pending
// matter; the create shape creates the matter already open, so one approval both creates and
// opens it. Either way it runs inside the gate's tx on clk's business date and publishes
// EventOpened. Any other payload fails, so nothing is applied half-way.
func (s *Store) OpeningEffect(clk clock.Clock) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		var c openingPayload
		if err := json.Unmarshal(g.Payload, &c); err != nil || c.MatterID == "" {
			return fmt.Errorf("matter.opening %s: no matter: %w", g.GateID, ErrInvalid)
		}
		if s.Events == nil || clk == nil {
			return fmt.Errorf("matter.opening %s: no event emitter or clock: %w", g.GateID, ErrInvalid)
		}
		by, err := openingApprover(g)
		if err != nil {
			return fmt.Errorf("matter.opening %s: %w", g.GateID, err)
		}
		on := clk.Today(ctx)
		switch {
		case c.Status == string(Open):
			err = s.openTx(ctx, tx, ID(c.MatterID), on, by)
		case c.createShape():
			err = s.createOpenTx(ctx, tx, c, on, preparerUser(g))
		default:
			err = fmt.Errorf("not a status change to open or a new matter: %w", ErrInvalid)
		}
		if err != nil {
			return fmt.Errorf("matter.opening %s: %w", g.GateID, err)
		}
		return nil
	}
}

// createOpenTx inserts the matter c describes, already open on on, and publishes EventOpened
// in tx. createdBy is the gate's preparer.
func (s *Store) createOpenTx(ctx context.Context, tx pgx.Tx, c openingPayload, on clock.Date, createdBy string) error {
	vis, err := validateMatter(NewMatter{Number: c.Number, ShortName: c.ShortName, ClientID: party.ContactID(c.ClientID),
		PracticeAreaID: c.PracticeAreaID, ResponsibleLawyerID: c.ResponsibleLawyerID, Visibility: Visibility(c.Visibility)})
	if err != nil {
		return err
	}
	ev := Opened{MatterID: c.MatterID, OpenedOn: on.String()}
	err = tx.QueryRow(ctx, createOpenSQL, c.MatterID, c.Number, c.ShortName, c.ClientID, c.PracticeAreaID,
		c.ResponsibleLawyerID, string(vis), DefaultRetentionClass, createdBy, on.String()).
		Scan(&ev.Number, &ev.ClientID, &ev.PracticeAreaID, &ev.ResponsibleUserID)
	if err != nil {
		return mapErr(err, ErrDuplicate)
	}
	// rule: ARC-I-030
	// The event commits with the new open matter.
	return s.Events.Emit(ctx, tx, outbox.Subject{Kind: matterkind.Subject, ID: c.MatterID}, EventOpened, ev)
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
		return s.changeStageTx(ctx, tx, c, gateID)
	})
	if err != nil {
		return fmt.Errorf("change stage %s: %w", c.MatterID, err)
	}
	return nil
}

// changeStageTx is ChangeStage inside the caller's tx.
func (s *Store) changeStageTx(ctx context.Context, tx pgx.Tx, c StageChange, gateID string) error {
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
	// The event commits with the stage change.
	body := StageChanged{MatterID: string(c.MatterID), FromStageID: c.FromStageID, ToStageID: c.ToStageID, GateID: gateID}
	return s.Events.Emit(ctx, tx, outbox.Subject{Kind: matterkind.Subject, ID: string(c.MatterID)}, EventStageChanged, body)
}
