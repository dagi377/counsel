package boot

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// ErrNotApprovable means an approved gate's subject row is missing or in a state its approval
// cannot move (a discarded draft, a message already approved under another gate).
var ErrNotApprovable = errors.New("legal boot: gate subject cannot be approved")

// execer is the part of pgx.Tx the approval effect uses.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Approval SQL per subject kind. Each moves the subject to approved and matches a row already
// approved by the same gate, so a re-run is a no-op (idempotent).
const (
	// A drafted proposal (tool-gap kinds: draft_document + submit_for_approval) becomes approved.
	sqlApproveDocDraft = `UPDATE docs.draft SET status = 'approved'
WHERE id = $1::uuid AND status IN ('ready','in_review','approved')`
	// An agent file note is confirmed by its approver.
	sqlApproveNote = `UPDATE matter.note SET confirmed_by = $2::uuid
WHERE id = $1::uuid AND (confirmed_by IS NULL OR confirmed_by = $2::uuid)`
	// An outbound message (G-B7 collections message, outgoing email) is stamped with its
	// approving gate and queued for delivery; the drafting run moves to drafted_by_agent_run_id.
	sqlApproveEmail = `UPDATE comms.comm_item SET approval_id = $2::uuid,
  drafted_by_agent_run_id = COALESCE(drafted_by_agent_run_id, agent_run_id),
  delivery_status = CASE WHEN direction = 'outbound' THEN COALESCE(delivery_status, 'queued') ELSE delivery_status END
WHERE id = $1::uuid AND (approval_id IS NULL OR approval_id = $2::uuid)`
)

// ApprovalEffect is the effect of an app gate kind whose approval releases the gate's subject:
// it moves a drafted document, agent note or outbound message to approved. A subject kind it
// does not move (a lead, a conflict run, a work item) is recorded by the gate decision itself,
// and the effect only re-checks who decided. It runs at most once per gate in the kernel's
// deciding tx, and each statement is idempotent besides.
func ApprovalEffect(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	return approve(ctx, tx, g)
}

func approve(ctx context.Context, tx execer, g registry.Approved) error {
	if err := checkDeciders(g); err != nil {
		return err
	}
	var (
		sql  string
		args []any
	)
	switch g.Subject.Kind {
	case SubjectDocumentDraft:
		sql, args = sqlApproveDocDraft, []any{g.Subject.ID}
	case agent.SubjectAgentNote:
		sql, args = sqlApproveNote, []any{g.Subject.ID, g.Approvers[0].UserID}
	case agent.SubjectEmailDraft:
		sql, args = sqlApproveEmail, []any{g.Subject.ID, g.GateID}
	default:
		return nil
	}
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("approve %s %s (gate %s): %w", g.Kind, g.Subject.Kind, g.GateID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("approve %s %s %s (gate %s): %w", g.Kind, g.Subject.Kind, g.Subject.ID, g.GateID, ErrNotApprovable)
	}
	return nil
}

// checkDeciders refuses an approval with no approver, an agent approver or the preparer (or
// the user an agent preparer acted for) among the approvers. The kernel refuses these when
// deciding; the effect fails closed on them as well.
func checkDeciders(g registry.Approved) error {
	if len(g.Approvers) == 0 {
		return fmt.Errorf("approve %s (gate %s): no approver: %w", g.Kind, g.GateID, ErrNotApprovable)
	}
	prep := g.Preparer.UserID
	if g.Preparer.Kind == registry.PrincipalAgent {
		prep = g.Preparer.ActingFor
	}
	for _, a := range g.Approvers {
		// rule: ARC-I-019
		if a.Kind != registry.PrincipalUser || a.UserID == "" {
			return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, gates.ErrAgentApproves)
		}
		if prep != "" && a.UserID == prep {
			return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, gates.ErrPreparerApproves)
		}
	}
	return nil
}

// ApprovalKinds are the app gate kinds whose effect is ApprovalEffect: every spec kind with no
// dedicated effect service (GATE-EFFECTS emptied unwired_gates.txt with them). A kind with a
// dedicated effect (bill approval, write-off, withdrawal, refund, receipt, signatory,
// matter.opening, matter.stage_move, and the GE2 money kinds in moneyEffects) is not here: without its ports it stays unregistered
// rather than approving with no posting.
var ApprovalKinds = []string{
	"billing.collections_message",
	"cal.calendar_delegation",
	"cal.change",
	"cal.confirm",
	"cal.mark_met",
	"cal.release",
	"cal.rule_pack",
	"cal.second_check",
	"conflict.clearance",
	"conflict.client_id",
	"conflict.consent_text",
	"conflict.id_destruction",
	"conflict.lateral",
	"conflict.wall",
	"docs.engagement_letter",
	"docs.filing_privilege",
	"docs.outgoing",
	"docs.precedent",
	"docs.template_publish",
	"intake.consult_outcome",
	"intake.fee_waiver",
	"intake.lead_decision",
	"intake.open_matter",
	"intake.public_wording",
	"matter.agent_note",
	"matter.apply_template",
	"matter.bulk_change",
	"matter.contact_merge",
	"matter.key_fact",
	"matter.party_conflict",
	"matter.practice_config",
	"matter.renumber",
	"matter.widen_access",
	"platform.access_change",
	"platform.closing",
	"platform.escrow_destination",
	"platform.export",
	"platform.file_destruction",
	"platform.guest_access",
	"platform.import_balances",
	"platform.integration_scope",
	"platform.outbound_connector",
	"platform.outgoing_email",
	"platform.payment_provider",
	"platform.portal_message",
	"platform.published_price",
	"platform.research_memo",
	"platform.rule_pack",
	"platform.security_answer",
	"platform.service_credit",
	"platform.skill_autonomy",
	"platform.summary_accuracy",
	"platform.vendor_support",
	"timekeeping.accept",
	"timekeeping.disbursement",
	"timekeeping.fee_arrangement",
	"timekeeping.timesheet",
	"timekeeping.writedown",
	"work.absence",
	"work.automation",
	"work.external_sync",
	"work.output",
	"work.plan",
	"work.workflow_publish",
}

// withApprovalEffects gives each ApprovalKinds kind that fx has no effect for the
// ApprovalEffect, so every spec kind (and every kind a feature Def declares) is registered.
func withApprovalEffects(fx map[string]registry.EffectFunc) (map[string]registry.EffectFunc, error) {
	out := make(map[string]registry.EffectFunc, len(fx)+len(ApprovalKinds))
	for k, f := range fx {
		out[k] = f
	}
	for _, k := range ApprovalKinds {
		if _, ok := out[k]; !ok {
			out[k] = ApprovalEffect
		}
	}
	return out, nil
}
