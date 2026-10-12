package deadlines

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/gate"
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
// every deadline effect writes a cal.deadline_confirmation row carrying the approving
// gate.gate_decision id (gates.Decision, never the gate id) and proves the change by that row. open opens the cal.change proposals of an approved rule pack (RulePackEffect).
func GateEffects(open ChangeOpener) map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{
		GateCalConfirm:     gates.Effect(applyConfirm),
		GateCalSecondCheck: gates.Effect(applySecondCheck),
		GateCalChange:      gates.Effect(applyChange),
		GateCalMarkMet:     gates.Effect(applyMarkMet),
		GateCalRelease:     gates.Effect(applyRelease),
		GateCalRulePack:    RulePackEffect(open),
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
ON CONFLICT (deadline_id, user_id, kind) WHERE kind <> 'change' DO NOTHING`
)

func applyConfirm(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	dec, err := gates.Decision(ctx, g)
	if err != nil {
		return err
	}
	if g.Subject.Kind != subjectDeadline && g.Subject.Kind != subjectDeadlineKind {
		return fmt.Errorf("%s gate %s: subject %q is not a deadline: %w", g.Kind, g.GateID, g.Subject.Kind, gates.ErrNotApplied)
	}
	id := g.Subject.ID
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: confirmSQL, Args: []any{id, by}},
		{SQL: confirmLogSQL, Args: []any{id, by, dec}},
	}, gates.Step{SQL: proofSQL, Args: []any{id, "confirm", dec}})
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
ON CONFLICT (deadline_id, user_id, kind) WHERE kind <> 'change' DO NOTHING`

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
	dec, err := gates.Decision(ctx, g)
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
		{SQL: secondCheckSQL, Args: []any{p.DeadlineID, by, p.DueOn, dec}},
	}, gates.Step{SQL: proofSQL, Args: []any{p.DeadlineID, "second_check", dec}})
}

// G7c: an open deadline moves to the approved due date, with the reason in the trail. The
// change log is unique per gate (deadline_change_once_per_gate), not per approver, so one
// deadline can change more than once.
const (
	changeSQL = `UPDATE cal.deadline SET due_on = $2::date, updated_by = $3::uuid, updated_at = now(),
  row_version = row_version + 1
WHERE id = $1::uuid AND status IN ('proposed','confirmed')
  AND NOT EXISTS (SELECT 1 FROM cal.deadline_confirmation c
                   WHERE c.deadline_id = $1::uuid AND c.kind = 'change' AND c.gate_decision_id = $4::uuid)`
	changeLogSQL = `INSERT INTO cal.deadline_confirmation (deadline_id, kind, user_id, independent_date, result, note, gate_decision_id)
SELECT d.id, 'change', $3::uuid, $2::date, 'done', $5, $4::uuid FROM cal.deadline d
 WHERE d.id = $1::uuid AND d.due_on = $2::date AND d.status IN ('proposed','confirmed')
ON CONFLICT (deadline_id, gate_decision_id) WHERE kind = 'change' DO NOTHING`
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
	dec, err := gates.Decision(ctx, g)
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
		{SQL: changeSQL, Args: []any{p.DeadlineID, p.DueOn, by, dec}},
		{SQL: changeLogSQL, Args: []any{p.DeadlineID, p.DueOn, by, dec, p.Reason}},
	}, gates.Step{SQL: proofSQL, Args: []any{p.DeadlineID, "change", dec}})
}

// G7d: a confirmed deadline is marked met on the approved evidence document.
const (
	markMetSQL = `UPDATE cal.deadline SET status = 'met', evidence_doc_id = $2::uuid, updated_by = $3::uuid,
  updated_at = now(), row_version = row_version + 1
WHERE id = $1::uuid AND status = 'confirmed'`
	markMetLogSQL = `INSERT INTO cal.deadline_confirmation (deadline_id, kind, user_id, result, gate_decision_id)
SELECT d.id, 'met', $3::uuid, 'done', $4::uuid FROM cal.deadline d
 WHERE d.id = $1::uuid AND d.status = 'met' AND d.evidence_doc_id = $2::uuid
ON CONFLICT (deadline_id, user_id, kind) WHERE kind <> 'change' DO NOTHING`
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
	dec, err := gates.Decision(ctx, g)
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
		{SQL: markMetLogSQL, Args: []any{p.DeadlineID, p.EvidenceDocumentID, by, dec}},
	}, gates.Step{SQL: proofSQL, Args: []any{p.DeadlineID, "met", dec}})
}

// G7r: an open deadline is released with the approved reason.
const (
	releaseSQL = `UPDATE cal.deadline SET status = 'released', release_reason = $2, updated_by = $3::uuid,
  updated_at = now(), row_version = row_version + 1
WHERE id = $1::uuid AND status IN ('proposed','confirmed')`
	releaseLogSQL = `INSERT INTO cal.deadline_confirmation (deadline_id, kind, user_id, result, note, gate_decision_id)
SELECT d.id, 'release', $3::uuid, 'done', $2, $4::uuid FROM cal.deadline d
 WHERE d.id = $1::uuid AND d.status = 'released' AND d.release_reason = $2
ON CONFLICT (deadline_id, user_id, kind) WHERE kind <> 'change' DO NOTHING`
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
	dec, err := gates.Decision(ctx, g)
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
		{SQL: releaseLogSQL, Args: []any{p.DeadlineID, p.Reason, by, dec}},
	}, gates.Step{SQL: proofSQL, Args: []any{p.DeadlineID, "release", dec}})
}

// G7e: the approved rule pack version becomes the active one for its code and the version
// it replaces is retired. The deadlines computed from another version of the pack are then
// re-computed from the approved one, and each whose due date moves gets a cal.change
// proposal (G7c) prepared for the pack's preparer. The effect never moves a date itself: a
// person approves each change. A deadline with a cal.change already pending is skipped, so
// a re-run opens nothing twice; a deadline whose rule the new version no longer has keeps
// its pinned version.
const (
	rulePackRetireSQL = `UPDATE cal.rule_pack SET status = 'retired'
WHERE code = $1 AND version <> $2 AND status = 'active'
  AND EXISTS (SELECT 1 FROM cal.rule_pack n WHERE n.code = $1 AND n.version = $2 AND n.status IN ('draft','verified'))`
	rulePackActivateSQL = `UPDATE cal.rule_pack SET status = 'active',
  verified_by = COALESCE(verified_by, $3::uuid), verified_on = COALESCE(verified_on, ` + todaySQL + `)
WHERE code = $1 AND version = $2 AND status IN ('draft','verified')`
	rulePackProofSQL = `SELECT 1 FROM cal.rule_pack WHERE code = $1 AND version = $2 AND status = 'active'`
	// rulePackAffectedSQL reads the active pack's rules and holidays and the open deadlines
	// computed from another version of it, as JSON text.
	rulePackAffectedSQL = `SELECT p.rules::text,
  COALESCE((SELECT json_agg(h.day ORDER BY h.day) FROM cal.holiday h WHERE h.rule_pack_id = p.id), '[]'::json)::text,
  COALESCE((SELECT json_agg(json_build_object('id', d.id, 'rule_key', d.rule_key, 'trigger_on', d.trigger_on,
                                              'due_on', d.due_on) ORDER BY d.id)
              FROM cal.deadline d
             WHERE d.pack_code = p.code AND d.pack_version IS DISTINCT FROM p.version
               AND d.status IN ('proposed','confirmed') AND d.rule_key IS NOT NULL AND d.trigger_on IS NOT NULL
               AND NOT EXISTS (SELECT 1 FROM gate.approval_request a
                                WHERE a.gate_code = '` + GateCalChange + `' AND a.payload_id = d.id AND a.status = 'pending')),
           '[]'::json)::text
  FROM cal.rule_pack p WHERE p.code = $1 AND p.version = $2 AND p.status = 'active'`
)

type rulePackPayload struct {
	Pack    string `json:"pack"`
	Version int    `json:"version"`
}

// ChangeOpener opens a gate in the deciding tx; gate.Engine is one. The rule pack effect
// opens its cal.change proposals through it.
type ChangeOpener interface {
	Open(ctx context.Context, tx pgx.Tx, kind string, subject registry.Subject, payload json.RawMessage, preparer registry.Principal) (gate.GateID, error)
}

// rulePackTx is the part of pgx.Tx the rule pack effect uses.
type rulePackTx interface {
	gates.Execer
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// openChange opens one cal.change proposal in tx.
type openChange func(ctx context.Context, tx rulePackTx, deadlineID string, payload json.RawMessage, preparer registry.Principal) error

// RulePackEffect is the cal.rule_pack approval effect; open opens the cal.change proposals.
// With a nil open the effect fails closed once a deadline needs a change.
func RulePackEffect(open ChangeOpener) registry.EffectFunc {
	var oc openChange
	if open != nil {
		oc = func(ctx context.Context, tx rulePackTx, id string, payload json.RawMessage, prep registry.Principal) error {
			ptx, ok := tx.(pgx.Tx)
			if !ok {
				return fmt.Errorf("open %s for deadline %s: tx is not a pgx.Tx: %w", GateCalChange, id, gates.ErrNotApplied)
			}
			_, err := open.Open(ctx, ptx, GateCalChange, registry.Subject{Kind: subjectDeadline, ID: id}, payload, prep)
			return err
		}
	}
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error { return applyRulePack(ctx, tx, g, oc) }
}

// packRule is one rule of cal.rule_pack.rules, in the rule pack file shape.
type packRule struct {
	ID          string `json:"id"`
	Trigger     string `json:"trigger"`
	OffsetDays  int    `json:"offset_days"`
	MonthsAfter int    `json:"months_after"`
	DayCount    string `json:"day_count"`
	Roll        string `json:"roll"`
}

// affectedDeadline is a deadline computed from another version of the approved pack.
type affectedDeadline struct {
	ID        string `json:"id"`
	RuleKey   string `json:"rule_key"`
	TriggerOn string `json:"trigger_on"`
	DueOn     string `json:"due_on"`
}

func applyRulePack(ctx context.Context, tx rulePackTx, g registry.Approved, open openChange) error {
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
	if err := gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: rulePackRetireSQL, Args: []any{p.Pack, v}},
		{SQL: rulePackActivateSQL, Args: []any{p.Pack, v, by}},
	}, gates.Step{SQL: rulePackProofSQL, Args: []any{p.Pack, v}}); err != nil {
		return err
	}
	var rules, holidays, affected string
	if err := tx.QueryRow(ctx, rulePackAffectedSQL, p.Pack, v).Scan(&rules, &holidays, &affected); err != nil {
		return fmt.Errorf("%s gate %s affected deadlines: %w", g.Kind, g.GateID, err)
	}
	changes, err := recompute(p.Pack, v, rules, holidays, affected)
	if err != nil {
		return fmt.Errorf("%s gate %s: %v: %w", g.Kind, g.GateID, err, gates.ErrNotApplied)
	}
	if len(changes) > 0 && open == nil {
		return fmt.Errorf("%s gate %s: %d deadlines need a change and no gate opener is set: %w", g.Kind, g.GateID, len(changes), gates.ErrNotApplied)
	}
	reason := fmt.Sprintf("Re-computed under rule pack %s version %s (gate %s)", p.Pack, v, g.GateID)
	for _, c := range changes {
		// A proposal, not a date change: the deadline moves only when a person approves it.
		payload, err := json.Marshal(changePayload{DeadlineID: c.ID, DueOn: c.DueOn, Reason: reason})
		if err != nil {
			return err
		}
		if err := open(ctx, tx, c.ID, payload, g.Preparer); err != nil {
			return fmt.Errorf("%s gate %s: open %s for deadline %s: %w", g.Kind, g.GateID, GateCalChange, c.ID, err)
		}
	}
	return nil
}

// recompute returns, for each affected deadline whose due date the pack moves, the deadline
// with its new due date, in the order given. A deadline whose rule the pack lacks is skipped.
func recompute(code, version, rulesJSON, holidaysJSON, affectedJSON string) ([]affectedDeadline, error) {
	var rules []packRule
	if err := json.Unmarshal([]byte(rulesJSON), &rules); err != nil {
		return nil, fmt.Errorf("rule pack %s %s rules: %v", code, version, err)
	}
	var days []string
	if err := json.Unmarshal([]byte(holidaysJSON), &days); err != nil {
		return nil, fmt.Errorf("rule pack %s %s holidays: %v", code, version, err)
	}
	var affected []affectedDeadline
	if err := json.Unmarshal([]byte(affectedJSON), &affected); err != nil {
		return nil, fmt.Errorf("rule pack %s %s deadlines: %v", code, version, err)
	}
	pack := deadline.Pack{ID: code, Version: version}
	for _, h := range days {
		d, err := parseDate(h)
		if err != nil {
			return nil, fmt.Errorf("rule pack %s %s holiday %q: %v", code, version, h, err)
		}
		pack.Holidays = append(pack.Holidays, d)
	}
	known := map[string]bool{}
	for _, r := range rules {
		roll := deadline.Roll(r.Roll)
		if roll == "" {
			roll = deadline.RollNone
		}
		pack.Rules = append(pack.Rules, deadline.Rule{ID: r.ID, Trigger: r.Trigger, Mode: deadline.DayCount(r.DayCount),
			Days: r.OffsetDays, MonthsAfter: r.MonthsAfter, Roll: roll})
		known[r.ID] = true
	}
	var out []affectedDeadline
	for _, a := range affected {
		if !known[a.RuleKey] {
			continue
		}
		trigger, err := parseDate(a.TriggerOn)
		if err != nil {
			return nil, fmt.Errorf("deadline %s trigger %q: %v", a.ID, a.TriggerOn, err)
		}
		due, err := deadline.Due(pack, a.RuleKey, trigger)
		if err != nil {
			return nil, fmt.Errorf("deadline %s: %v", a.ID, err)
		}
		if due.String() != a.DueOn {
			out = append(out, affectedDeadline{ID: a.ID, RuleKey: a.RuleKey, TriggerOn: a.TriggerOn, DueOn: due.String()})
		}
	}
	return out, nil
}

// parseDate parses a YYYY-MM-DD date.
func parseDate(s string) (deadline.Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return deadline.Date{}, err
	}
	return deadline.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, nil
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
