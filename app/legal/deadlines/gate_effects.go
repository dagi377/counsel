package deadlines

import (
	"context"
	"fmt"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// Deadline gate kinds whose approval this package applies.
const (
	GateCalConfirm      = "cal.confirm"
	GateCalChange       = "cal.change"
	GateCalMarkMet      = "cal.mark_met"
	GateCalRelease      = "cal.release"
	GateCalRulePack     = "cal.rule_pack"
	GateCalDelegation   = "cal.calendar_delegation"
	subjectDeadline     = "deadline"          // agent propose_deadline gates
	subjectDeadlineKind = "deadline.deadline" // kernel deadline route gates
)

// todaySQL is today's date in the firm's time zone.
const todaySQL = `(now() AT TIME ZONE 'America/Toronto')::date`

// GateEffects are the approval effects of the deadline and calendar gate kinds. Each runs in
// the deciding tx, refuses an agent approver or the preparer, and is idempotent per gate:
// every deadline effect writes a cal.deadline_confirmation row carrying the gate id and
// proves the change by that row.
func GateEffects() map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{
		GateCalConfirm:     gates.Effect(applyConfirm),
		GateCalSecondCheck: gates.Effect(applySecondCheck),
		GateCalChange:      gates.Effect(applyChange),
		GateCalMarkMet:     gates.Effect(applyMarkMet),
		GateCalRelease:     gates.Effect(applyRelease),
		GateCalRulePack:    gates.Effect(applyRulePack),
		GateCalDelegation:  gates.Effect(applyDelegation),
	}
}

// proofSQL holds once the deadline carries a confirmation of kind $2 written by gate $3.
const proofSQL = `SELECT 1 FROM cal.deadline_confirmation
 WHERE deadline_id = $1::uuid AND kind = $2 AND gate_decision_id = $3::uuid`

// G7: a proposed deadline becomes confirmed by its approver.
const (
	confirmSQL = `UPDATE cal.deadline SET status = 'confirmed', updated_by = $2::uuid, updated_at = now(),
  row_version = row_version + 1
WHERE id = $1::uuid AND status = 'proposed'`
	confirmLogSQL = `INSERT INTO cal.deadline_confirmation (deadline_id, kind, user_id, independent_date, result, gate_decision_id)
SELECT d.id, 'confirm', $2::uuid, d.due_on, 'agree', $3::uuid FROM cal.deadline d
 WHERE d.id = $1::uuid AND d.status = 'confirmed'
ON CONFLICT (deadline_id, user_id, kind) DO NOTHING`
)

func applyConfirm(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	if g.Subject.Kind != subjectDeadline && g.Subject.Kind != subjectDeadlineKind {
		return fmt.Errorf("%s gate %s: subject %q is not a deadline: %w", g.Kind, g.GateID, g.Subject.Kind, gates.ErrNotApplied)
	}
	id := g.Subject.ID
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: confirmSQL, Args: []any{id, by}},
		{SQL: confirmLogSQL, Args: []any{id, by, g.GateID}},
	}, gates.Step{SQL: proofSQL, Args: []any{id, "confirm", g.GateID}})
}

// G7b: a second person records an independent due date. The check agrees only when the
// independent date matches; the confirmer may not second-check their own confirmation.
const secondCheckSQL = `INSERT INTO cal.deadline_confirmation (deadline_id, kind, user_id, independent_date, result, gate_decision_id)
SELECT d.id, 'second_check', $2::uuid, $3::date,
       CASE WHEN d.due_on = $3::date THEN 'agree' ELSE 'disagree' END, $4::uuid
  FROM cal.deadline d
 WHERE d.id = $1::uuid AND d.status IN ('proposed','confirmed')
   AND NOT EXISTS (SELECT 1 FROM cal.deadline_confirmation c
                    WHERE c.deadline_id = d.id AND c.kind = 'confirm' AND c.user_id = $2::uuid)
ON CONFLICT (deadline_id, user_id, kind) DO NOTHING`

type secondCheckPayload struct {
	DeadlineID string `json:"deadline_id"`
	DueOn      string `json:"due_on"`
}

func applySecondCheck(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p secondCheckPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"deadline_id": p.DeadlineID, "due_on": p.DueOn}); err != nil {
		return err
	}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: secondCheckSQL, Args: []any{p.DeadlineID, by, p.DueOn, g.GateID}},
	}, gates.Step{SQL: proofSQL, Args: []any{p.DeadlineID, "second_check", g.GateID}})
}

// G7c: an open deadline moves to the approved due date, with the reason in the trail.
const (
	changeSQL = `UPDATE cal.deadline SET due_on = $2::date, updated_by = $3::uuid, updated_at = now(),
  row_version = row_version + 1
WHERE id = $1::uuid AND status IN ('proposed','confirmed')
  AND NOT EXISTS (SELECT 1 FROM cal.deadline_confirmation c
                   WHERE c.deadline_id = $1::uuid AND c.kind = 'change' AND c.gate_decision_id = $4::uuid)`
	changeLogSQL = `INSERT INTO cal.deadline_confirmation (deadline_id, kind, user_id, independent_date, result, note, gate_decision_id)
SELECT d.id, 'change', $3::uuid, $2::date, 'done', $5, $4::uuid FROM cal.deadline d
 WHERE d.id = $1::uuid AND d.due_on = $2::date AND d.status IN ('proposed','confirmed')
ON CONFLICT (deadline_id, user_id, kind) DO NOTHING`
)

type changePayload struct {
	DeadlineID string `json:"deadline_id"`
	DueOn      string `json:"due_on"`
	Reason     string `json:"reason"`
}

func applyChange(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p changePayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"deadline_id": p.DeadlineID, "due_on": p.DueOn, "reason": p.Reason}); err != nil {
		return err
	}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: changeSQL, Args: []any{p.DeadlineID, p.DueOn, by, g.GateID}},
		{SQL: changeLogSQL, Args: []any{p.DeadlineID, p.DueOn, by, g.GateID, p.Reason}},
	}, gates.Step{SQL: proofSQL, Args: []any{p.DeadlineID, "change", g.GateID}})
}

// G7d: a confirmed deadline is marked met on the approved evidence document.
const (
	markMetSQL = `UPDATE cal.deadline SET status = 'met', evidence_doc_id = $2::uuid, updated_by = $3::uuid,
  updated_at = now(), row_version = row_version + 1
WHERE id = $1::uuid AND status = 'confirmed'`
	markMetLogSQL = `INSERT INTO cal.deadline_confirmation (deadline_id, kind, user_id, result, gate_decision_id)
SELECT d.id, 'met', $3::uuid, 'done', $4::uuid FROM cal.deadline d
 WHERE d.id = $1::uuid AND d.status = 'met' AND d.evidence_doc_id = $2::uuid
ON CONFLICT (deadline_id, user_id, kind) DO NOTHING`
)

type markMetPayload struct {
	DeadlineID         string `json:"deadline_id"`
	EvidenceDocumentID string `json:"evidence_document_id"`
}

func applyMarkMet(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p markMetPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"deadline_id": p.DeadlineID, "evidence_document_id": p.EvidenceDocumentID}); err != nil {
		return err
	}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: markMetSQL, Args: []any{p.DeadlineID, p.EvidenceDocumentID, by}},
		{SQL: markMetLogSQL, Args: []any{p.DeadlineID, p.EvidenceDocumentID, by, g.GateID}},
	}, gates.Step{SQL: proofSQL, Args: []any{p.DeadlineID, "met", g.GateID}})
}

// G7r: an open deadline is released with the approved reason.
const (
	releaseSQL = `UPDATE cal.deadline SET status = 'released', release_reason = $2, updated_by = $3::uuid,
  updated_at = now(), row_version = row_version + 1
WHERE id = $1::uuid AND status IN ('proposed','confirmed')`
	releaseLogSQL = `INSERT INTO cal.deadline_confirmation (deadline_id, kind, user_id, result, note, gate_decision_id)
SELECT d.id, 'release', $3::uuid, 'done', $2, $4::uuid FROM cal.deadline d
 WHERE d.id = $1::uuid AND d.status = 'released' AND d.release_reason = $2
ON CONFLICT (deadline_id, user_id, kind) DO NOTHING`
)

type releasePayload struct {
	DeadlineID string `json:"deadline_id"`
	Reason     string `json:"reason"`
}

func applyRelease(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p releasePayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"deadline_id": p.DeadlineID, "reason": p.Reason}); err != nil {
		return err
	}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: releaseSQL, Args: []any{p.DeadlineID, p.Reason, by}},
		{SQL: releaseLogSQL, Args: []any{p.DeadlineID, p.Reason, by, g.GateID}},
	}, gates.Step{SQL: proofSQL, Args: []any{p.DeadlineID, "release", g.GateID}})
}

// G7e: the approved rule pack version becomes the active one for its code and the version
// it replaces is retired. Deadlines stay pinned to the version they were computed with.
const (
	rulePackRetireSQL = `UPDATE cal.rule_pack SET status = 'retired'
WHERE code = $1 AND version <> $2 AND status = 'active'
  AND EXISTS (SELECT 1 FROM cal.rule_pack n WHERE n.code = $1 AND n.version = $2 AND n.status IN ('draft','verified'))`
	rulePackActivateSQL = `UPDATE cal.rule_pack SET status = 'active',
  verified_by = COALESCE(verified_by, $3::uuid), verified_on = COALESCE(verified_on, ` + todaySQL + `)
WHERE code = $1 AND version = $2 AND status IN ('draft','verified')`
	rulePackProofSQL = `SELECT 1 FROM cal.rule_pack WHERE code = $1 AND version = $2 AND status = 'active'`
)

type rulePackPayload struct {
	Pack    string `json:"pack"`
	Version int    `json:"version"`
}

func applyRulePack(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p rulePackPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if p.Version < 1 {
		return fmt.Errorf("%s gate %s payload: no version: %w", g.Kind, g.GateID, gates.ErrNotApplied)
	}
	if err := gates.Need(g, map[string]string{"pack": p.Pack}); err != nil {
		return err
	}
	v := fmt.Sprint(p.Version)
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: rulePackRetireSQL, Args: []any{p.Pack, v}},
		{SQL: rulePackActivateSQL, Args: []any{p.Pack, v, by}},
	}, gates.Step{SQL: rulePackProofSQL, Args: []any{p.Pack, v}})
}

// G-CAL: the calendar owner's personal calendars are shared with the delegate at edit level,
// and feed_shows_titles sets whether their feed shows event titles (details) or busy only.
const (
	delegateShareSQL = `INSERT INTO cal.calendar_share (calendar_id, user_id, level, granted_by)
SELECT c.id, $2::uuid, 'edit', $3::uuid FROM cal.calendar c
 WHERE c.owner_user_id = $1::uuid AND c.kind = 'personal'
   AND NOT EXISTS (SELECT 1 FROM cal.calendar_share s WHERE s.calendar_id = c.id AND s.user_id = $2::uuid)`
	delegateFeedSQL = `UPDATE cal.calendar SET default_visibility = CASE WHEN $2 THEN 'details' ELSE 'busy_only' END
WHERE owner_user_id = $1::uuid AND kind = 'personal'`
	delegateShareProofSQL = `SELECT 1 FROM cal.calendar c JOIN cal.calendar_share s ON s.calendar_id = c.id
 WHERE c.owner_user_id = $1::uuid AND c.kind = 'personal' AND s.user_id = $2::uuid`
	delegateFeedProofSQL = `SELECT 1 FROM cal.calendar
 WHERE owner_user_id = $1::uuid AND kind = 'personal'
   AND default_visibility = CASE WHEN $2 THEN 'details' ELSE 'busy_only' END`
)

type delegationPayload struct {
	CalendarOwnerID string `json:"calendar_owner_id"`
	DelegateUserID  string `json:"delegate_user_id"`
	FeedShowsTitles *bool  `json:"feed_shows_titles"`
}

func applyDelegation(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p delegationPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"calendar_owner_id": p.CalendarOwnerID}); err != nil {
		return err
	}
	if p.DelegateUserID == "" && p.FeedShowsTitles == nil {
		return fmt.Errorf("%s gate %s payload: no delegate or feed change: %w", g.Kind, g.GateID, gates.ErrNotApplied)
	}
	if p.DelegateUserID == p.CalendarOwnerID {
		return fmt.Errorf("%s gate %s: owner cannot be their own delegate: %w", g.Kind, g.GateID, gates.ErrNotApplied)
	}
	var steps []gates.Step
	var proof gates.Step
	if p.FeedShowsTitles != nil {
		steps = append(steps, gates.Step{SQL: delegateFeedSQL, Args: []any{p.CalendarOwnerID, *p.FeedShowsTitles}})
		proof = gates.Step{SQL: delegateFeedProofSQL, Args: []any{p.CalendarOwnerID, *p.FeedShowsTitles}}
	}
	if p.DelegateUserID != "" {
		steps = append(steps, gates.Step{SQL: delegateShareSQL, Args: []any{p.CalendarOwnerID, p.DelegateUserID, by}})
		proof = gates.Step{SQL: delegateShareProofSQL, Args: []any{p.CalendarOwnerID, p.DelegateUserID}}
	}
	return gates.Apply(ctx, tx, g, steps, proof)
}
