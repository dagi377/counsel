package intake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateFeeWaiver is the G-fee gate kind (spec/gates.json).
const GateFeeWaiver = "intake.fee_waiver"

const (
	// G3: the lead's latest consultation records the approved outcome, signed off by the
	// approver. A re-run re-matches the same outcome and signer.
	sqlConsultOutcome = `UPDATE intake.consultation SET outcome = $2, signed_off_by = $3::uuid,
  signed_off_at = COALESCE(signed_off_at, $4)
WHERE id = (SELECT id FROM intake.consultation WHERE lead_id = $1::uuid AND status <> 'cancelled'
            ORDER BY starts_at DESC, id DESC LIMIT 1)
  AND (outcome IS NULL OR (outcome = $2 AND signed_off_by = $3::uuid))`
	// G-fee waive: a due consultation fee of exactly the approved amount is waived.
	sqlFeeWaive = `UPDATE intake.consultation SET fee_status = 'waived'
WHERE lead_id = $1::uuid AND fee_cents = $2 AND fee_status IN ('due','waived')`
	// G-fee refund: a paid consultation fee of exactly the approved amount is marked refunded
	// and the lead's payment status follows. The money moves through billing.refund.
	sqlFeeRefund = `WITH c AS (
  UPDATE intake.consultation SET fee_status = 'refunded'
   WHERE lead_id = $1::uuid AND fee_cents = $2 AND fee_status IN ('paid','refunded') RETURNING lead_id)
UPDATE intake.lead l SET payment_status = 'refunded', updated_by = $3::uuid, updated_at = $4
  FROM (SELECT DISTINCT lead_id FROM c) c WHERE l.id = c.lead_id`
	// G-open: read the accepted lead the matter is opened for.
	sqlOpenLead = `SELECT contact_id::text, COALESCE(decision, ''), COALESCE(converted_matter_id::text, ''),
  COALESCE(practice_area_id::text, '')
FROM intake.lead WHERE id = $1::uuid AND archived_at IS NULL FOR UPDATE`
	// G-open: the lead records the matter it converted into.
	sqlLeadConverted = `UPDATE intake.lead SET converted_matter_id = $2::uuid, updated_by = $3::uuid, updated_at = $4,
  row_version = row_version + 1
WHERE id = $1::uuid AND converted_matter_id IS NULL AND decision = 'accept'`
)

// ConsultOutcomeEffect is the intake.consult_outcome (G3) effect: the lead's consultation
// records the approved engage, decline or refer outcome, signed off by the approver.
func ConsultOutcomeEffect(now func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return consultOutcome(ctx, tx, g, now)
	}
}

func consultOutcome(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time) error {
	// rule: ARC-I-016
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p struct {
		LeadID  string `json:"lead_id"`
		Outcome string `json:"outcome"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	switch p.Outcome {
	case "engage", "decline", "refer":
	default:
		return gates.Refuse(g, "outcome")
	}
	if p.LeadID == "" {
		return gates.Refuse(g, "no lead")
	}
	// rule: ARC-I-012
	return gates.ExecOne(ctx, tx, g, sqlConsultOutcome, p.LeadID, p.Outcome, by, now())
}

// FeeWaiverEffect is the intake.fee_waiver (G-fee) effect: the consultation fee of exactly
// the approved amount (integer cents) is waived, or marked refunded.
func FeeWaiverEffect(now func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return feeWaiver(ctx, tx, g, now)
	}
}

func feeWaiver(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time) error {
	// rule: ARC-I-016
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p struct {
		LeadID      string `json:"lead_id"`
		AmountCents *int64 `json:"amount_cents"`
		Action      string `json:"action"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.LeadID == "" || p.AmountCents == nil || *p.AmountCents < 0 {
		return gates.Refuse(g, "lead and amount")
	}
	// rule: ARC-I-012
	// The approved amount is integer cents and must equal the fee it waives or refunds.
	switch p.Action {
	case "waive":
		return gates.ExecOne(ctx, tx, g, sqlFeeWaive, p.LeadID, *p.AmountCents)
	case "refund":
		return gates.ExecOne(ctx, tx, g, sqlFeeRefund, p.LeadID, *p.AmountCents, by, now())
	}
	return gates.Refuse(g, "action")
}

// openMatterPayload is the intake.open_matter payload: the spec fields plus the new matter's
// identity, which the server-side payload builder fills (additive, spec/gates.json).
type openMatterPayload struct {
	LeadID              string `json:"lead_id"`
	ResponsibleLawyerID string `json:"responsible_lawyer_id"`
	MatterID            string `json:"matter_id"`
	Number              string `json:"number"`
	ShortName           string `json:"short_name"`
	PracticeAreaID      string `json:"practice_area_id"`
	Visibility          string `json:"visibility"`
}

// OpenMatterEffect is the intake.open_matter (G-open) effect: the accepted lead's matter is
// created and opened through the matter.opening effect (opening), in the same tx, and the
// lead records the matter. A re-run finds the lead converted to that matter and does nothing.
func OpenMatterEffect(now func() time.Time, opening registry.EffectFunc) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		if opening == nil {
			return gates.Refuse(g, "no matter.opening effect")
		}
		return openMatter(ctx, tx, g, now, func(ctx context.Context, og registry.Approved) error {
			return opening(ctx, tx, og)
		})
	}
}

func openMatter(ctx context.Context, tx gates.Querier, g registry.Approved, now func() time.Time, open func(context.Context, registry.Approved) error) error {
	// rule: ARC-I-016
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p openMatterPayload
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.LeadID == "" || p.ResponsibleLawyerID == "" || p.MatterID == "" || p.Number == "" || p.ShortName == "" {
		return gates.Refuse(g, "lead, lawyer and matter identity")
	}
	var client, decision, converted, area string
	err = tx.QueryRow(ctx, sqlOpenLead, p.LeadID).Scan(&client, &decision, &converted, &area)
	if errors.Is(err, pgx.ErrNoRows) {
		return gates.Refuse(g, "no lead")
	}
	if err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	if converted == p.MatterID {
		return nil // this approval already opened the matter
	}
	if converted != "" || decision != "accept" {
		return gates.Refuse(g, "lead is not an accepted, unconverted lead")
	}
	if p.PracticeAreaID == "" {
		p.PracticeAreaID = area
	}
	if p.Visibility == "" {
		p.Visibility = "firm_open"
	}
	body, err := json.Marshal(map[string]string{
		"matter_id": p.MatterID, "number": p.Number, "short_name": p.ShortName, "client_id": client,
		"practice_area_id": p.PracticeAreaID, "responsible_lawyer_id": p.ResponsibleLawyerID, "visibility": p.Visibility,
	})
	if err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	og := g
	og.Kind, og.Payload = "matter.opening", body
	// rule: ARC-I-012
	// The matter is created and opened only by the matter.opening effect.
	if err := open(ctx, og); err != nil {
		return err
	}
	return gates.ExecOne(ctx, tx, g, sqlLeadConverted, p.LeadID, p.MatterID, by, now())
}
