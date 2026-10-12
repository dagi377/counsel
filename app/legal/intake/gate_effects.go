package intake

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// Gate effect SQL. Each statement matches the row in the state the approval moves it from and
// in the state the same approval already left it in, so a re-run changes nothing.
const (
	// G1: the lead records the approved decision and who decided it.
	sqlLeadDecision = `UPDATE intake.lead SET decision = $2, decided_by = $3::uuid,
  decided_at = COALESCE(decided_at, $4),
  updated_by = CASE WHEN decision IS NULL THEN $3::uuid ELSE updated_by END,
  updated_at = CASE WHEN decision IS NULL THEN $4 ELSE updated_at END,
  row_version = row_version + CASE WHEN decision IS NULL THEN 1 ELSE 0 END
WHERE id = $1::uuid AND archived_at IS NULL
  AND (decision IS NULL OR (decision = $2 AND decided_by = $3::uuid))`
	// G-form: the draft form goes live, approved by the approver.
	sqlPublishForm = `UPDATE intake.intake_form SET status = 'published', approved_by = $2::uuid,
  approved_at = CASE WHEN status = 'draft' THEN $3 ELSE approved_at END,
  updated_by = CASE WHEN status = 'draft' THEN $2::uuid ELSE updated_by END,
  updated_at = CASE WHEN status = 'draft' THEN $3 ELSE updated_at END,
  row_version = row_version + CASE WHEN status = 'draft' THEN 1 ELSE 0 END
WHERE id = $1::uuid AND (status = 'draft' OR (status = 'published' AND approved_by = $2::uuid))`
)

// leadDecisionPayload is the intake.lead_decision payload (spec/gates.json).
type leadDecisionPayload struct {
	LeadID   string `json:"lead_id"`
	Decision string `json:"decision"`
}

// publicWordingPayload is the intake.public_wording payload (spec/gates.json).
type publicWordingPayload struct {
	FormID  string `json:"form_id"`
	Version int    `json:"version"`
}

// LeadDecisionEffect is the intake.lead_decision (G1) effect: the lead records the approved
// accept, decline or refer decision, decided by the approver at now().
func LeadDecisionEffect(now func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return leadDecision(ctx, tx, g, now)
	}
}

func leadDecision(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p leadDecisionPayload
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	switch p.Decision {
	case "accept", "decline", "refer":
	default:
		return fmt.Errorf("approve %s (gate %s): decision %q: %w", g.Kind, g.GateID, p.Decision, gates.ErrNotApprovable)
	}
	if p.LeadID == "" {
		return fmt.Errorf("approve %s (gate %s): no lead: %w", g.Kind, g.GateID, gates.ErrNotApprovable)
	}
	// rule: ARC-I-012
	// The decision is gate-owned: only this effect sets it.
	return gates.ExecOne(ctx, tx, g, sqlLeadDecision, p.LeadID, p.Decision, by, now())
}

// PublicWordingEffect is the intake.public_wording (G-form) effect: the draft intake form is
// published, approved by the approver at now().
func PublicWordingEffect(now func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return publicWording(ctx, tx, g, now)
	}
}

func publicWording(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p publicWordingPayload
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.FormID == "" {
		return fmt.Errorf("approve %s (gate %s): no form: %w", g.Kind, g.GateID, gates.ErrNotApprovable)
	}
	// rule: ARC-I-012
	return gates.ExecOne(ctx, tx, g, sqlPublishForm, p.FormID, by, now())
}
