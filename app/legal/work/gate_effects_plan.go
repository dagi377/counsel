package work

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateExternalSync is the G-W4 gate kind (spec/gates.json).
const GateExternalSync = "work.external_sync"

const (
	// G-W3: the approved version of the workflow is released. A release by this gate already
	// there is re-matched by the probe below.
	sqlWorkflowRelease = `INSERT INTO work.workflow_release (workflow_id, version, gate_id, published_by, published_at)
SELECT id, version, $3::uuid, $4::uuid, $5 FROM work.workflow WHERE id = $1::uuid AND version = $2
ON CONFLICT (workflow_id) DO UPDATE SET version = EXCLUDED.version, gate_id = EXCLUDED.gate_id,
  published_by = EXCLUDED.published_by, published_at = EXCLUDED.published_at
WHERE work.workflow_release.version < EXCLUDED.version`
	sqlWorkflowReleased = `UPDATE work.workflow_release SET version = version WHERE workflow_id = $1::uuid AND gate_id = $2::uuid`
	// The workflow exists at another version than the approved one: the approval is stale.
	sqlWorkflowStale = `UPDATE work.workflow SET version = version WHERE id = $1::uuid AND version <> $2`
	// G-W4: sync of the matter to the integration is enabled.
	sqlExternalSync = `INSERT INTO work.external_sync (matter_id, integration_id, gate_id, enabled_by, enabled_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5) ON CONFLICT DO NOTHING`
	// G-W5: the absence is recorded once per gate.
	sqlAbsence = `INSERT INTO work.absence (gate_id, user_id, start_on, end_on, approved_by, approved_at)
VALUES ($1::uuid, $2::uuid, $3::date, $4::date, $5::uuid, $6) ON CONFLICT (gate_id) DO NOTHING`
	// G-W2: the plan is accepted once per gate and, in the same statement, every template item
	// becomes a task on the matter in the first to-do status of its workflow (the item's own,
	// or the latest released workflow that applies to tasks). A re-run inserts no acceptance
	// and so creates no task.
	sqlAcceptPlan = `WITH acc AS (
  INSERT INTO work.plan_acceptance (gate_id, matter_id, template_id, accepted_by, accepted_at)
  SELECT $1::uuid, $2::uuid, t.id, $4::uuid, $5 FROM work.work_template t WHERE t.id = $3::uuid
  ON CONFLICT (gate_id) DO NOTHING
  RETURNING template_id),
items AS (
  SELECT it.value AS item, it.ordinality AS n,
         COALESCE(NULLIF(it.value->>'workflow_id', '')::uuid,
                  (SELECT w.id FROM work.workflow w JOIN work.workflow_release r ON r.workflow_id = w.id
                    WHERE 'task' = ANY (w.applies_to) ORDER BY w.version DESC, w.id LIMIT 1)) AS workflow_id
    FROM acc JOIN work.work_template t ON t.id = acc.template_id,
         jsonb_array_elements(t.items) WITH ORDINALITY it)
INSERT INTO work.work_item (key, type, title, workflow_id, status_id, matter_id, subject_kind, subject_id,
  reporter_id, template_id, rank, due_at)
SELECT 'W-' || upper(right(replace(core.uuidv7()::text, '-', ''), 12)),
       COALESCE(NULLIF(i.item->>'type', ''), 'task'), i.item->>'title', i.workflow_id,
       (SELECT s.id FROM work.status s WHERE s.workflow_id = i.workflow_id AND s.status_group = 'todo'
         ORDER BY s.name LIMIT 1),
       $2::uuid, 'matter', $2::uuid, $4::uuid, $3::uuid, lpad(i.n::text, 6, '0'),
       CASE WHEN i.item ? 'due_days' THEN $5 + make_interval(days => (i.item->>'due_days')::int) END
  FROM items i`
	sqlPlanAccepted = `UPDATE work.plan_acceptance SET gate_id = gate_id WHERE gate_id = $1::uuid`
	// Offboarding hand-over: the leaver's open items move to the hand-over user.
	sqlReassignOpen = `UPDATE work.work_item w SET assignee_user_id = $2::uuid, updated_at = $3, row_version = w.row_version + 1
  FROM work.status s
 WHERE w.assignee_user_id = $1::uuid AND w.archived_at IS NULL AND s.id = w.status_id AND s.status_group <> 'done'
   AND w.approver_id IS DISTINCT FROM $2::uuid`
)

// PlanEffect is the work.plan (G-W2) effect: accepting the plan creates the template's
// proposed tasks on the matter, once.
func PlanEffect(now func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return acceptPlan(ctx, tx, g, now)
	}
}

func acceptPlan(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time) error {
	// rule: ARC-I-016
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p struct {
		MatterID   string `json:"matter_id"`
		TemplateID string `json:"template_id"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.MatterID == "" || p.TemplateID == "" {
		return gates.Refuse(g, "matter and template")
	}
	// rule: ARC-I-012
	// The agent cannot start template items until the plan is accepted here.
	if _, err := tx.Exec(ctx, sqlAcceptPlan, g.GateID, p.MatterID, p.TemplateID, by, now()); err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	return gates.ExecOne(ctx, tx, g, sqlPlanAccepted, g.GateID)
}

// WorkflowPublishEffect is the work.workflow_publish (G-W3) effect: the approved workflow
// version is released for use. A workflow at another version is a stale approval.
func WorkflowPublishEffect(now func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return publishWorkflow(ctx, tx, g, now)
	}
}

func publishWorkflow(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time) error {
	// rule: ARC-I-016
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p struct {
		WorkflowID string `json:"workflow_id"`
		Version    int    `json:"version"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.WorkflowID == "" || p.Version < 1 {
		return gates.Refuse(g, "workflow and version")
	}
	// rule: ARC-I-012
	if ok, err := gates.Matches(ctx, tx, g, sqlWorkflowRelease, p.WorkflowID, p.Version, g.GateID, by, now()); err != nil || ok {
		return err
	}
	if ok, err := gates.Matches(ctx, tx, g, sqlWorkflowReleased, p.WorkflowID, g.GateID); err != nil || ok {
		return err
	}
	// rule: ARC-I-014
	stale, err := gates.Matches(ctx, tx, g, sqlWorkflowStale, p.WorkflowID, p.Version)
	if err != nil {
		return err
	}
	if stale {
		return fmt.Errorf("approve %s (gate %s): workflow version %d: %w", g.Kind, g.GateID, p.Version, gates.ErrStale)
	}
	return gates.Refuse(g, "no such workflow, or a newer version is released")
}

// ExternalSyncEffect is the work.external_sync (G-W4) effect: the matter's sync to the
// integration is enabled. It needs the spec's two approvers.
func ExternalSyncEffect(now func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return externalSync(ctx, tx, g, now)
	}
}

func externalSync(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time) error {
	// rule: ARC-I-016
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	// rule: ARC-I-019
	if err := gates.MinApprovers(g, 2); err != nil {
		return err
	}
	var p struct {
		MatterID      string `json:"matter_id"`
		IntegrationID string `json:"integration_id"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.MatterID == "" || p.IntegrationID == "" {
		return gates.Refuse(g, "matter and integration")
	}
	// rule: ARC-I-012
	// No matter data leaves to the external tracker until this row exists.
	_, err = tx.Exec(ctx, sqlExternalSync, p.MatterID, p.IntegrationID, g.GateID, by, now())
	if err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	return nil
}

// AbsenceEffect is the work.absence (G-W5) effect: the approved absence is recorded.
func AbsenceEffect(now func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return approveAbsence(ctx, tx, g, now)
	}
}

func approveAbsence(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time) error {
	var p struct {
		UserID  string `json:"user_id"`
		StartOn string `json:"start_on"`
		EndOn   string `json:"end_on"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	// rule: ARC-I-016
	// The absent person never approves their own absence.
	by, err := gates.ApproverNot(g, p.UserID)
	if err != nil {
		return err
	}
	start, err1 := time.Parse(time.DateOnly, p.StartOn)
	end, err2 := time.Parse(time.DateOnly, p.EndOn)
	if p.UserID == "" || err1 != nil || err2 != nil || end.Before(start) {
		return gates.Refuse(g, "user and date range")
	}
	// rule: ARC-I-012
	if _, err := tx.Exec(ctx, sqlAbsence, g.GateID, p.UserID, p.StartOn, p.EndOn, by, now()); err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	return nil
}

// ReassignOpen moves every open work item assigned to from onto to (offboarding hand-over).
// An item whose approver is to keeps its assignee, since assignee and approver must differ.
func ReassignOpen(ctx context.Context, tx gates.Execer, from, to string, at time.Time) error {
	if _, err := tx.Exec(ctx, sqlReassignOpen, from, to, at); err != nil {
		return fmt.Errorf("reassign open items: %w", err)
	}
	return nil
}
