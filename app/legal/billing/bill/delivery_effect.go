package bill

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/registry"
)

// KindDelivery is the gate that records a bill's delivery on evidence (G-B2 delivery).
const KindDelivery = "billing.bill_delivery"

// Deliverer is the part of the bill store the delivery effect uses. Both methods run in the
// gate tx. PGService implements it.
type Deliverer interface {
	// LockedBill locks bill id and returns it, or ErrNotFound.
	LockedBill(ctx context.Context, tx pgx.Tx, id ID) (Bill, error)
	// Deliver records delivery evidence (PGService.Deliver).
	Deliver(ctx context.Context, tx pgx.Tx, id ID, ev DeliveryEvidence) (Bill, error)
}

// LockedBill implements Deliverer.
func (s *PGService) LockedBill(ctx context.Context, tx pgx.Tx, id ID) (Bill, error) {
	return s.lock(ctx, tx, "deliver", id)
}

var _ Deliverer = (*PGService)(nil)

// deliveryPayload is the billing.bill_delivery gate payload.
type deliveryPayload struct {
	BillID      string `json:"bill_id"`
	Channel     string `json:"channel"`
	DeliveredOn string `json:"delivered_on"` // YYYY-MM-DD, America/Toronto
	EvidenceRef string `json:"evidence_ref"`
}

// deliveryChannels maps the gate's channel to the stored delivery_channel: a printed bill is
// delivered by mail.
var deliveryChannels = map[string]Channel{"email": ChannelEmail, "portal": ChannelPortal, "print": ChannelMail}

// DeliveryEffect is the billing.bill_delivery EffectFunc: in the gate tx it marks the approved,
// signed bill delivered on the approved date and channel (bill.delivered_at is gate-owned).
type DeliveryEffect struct {
	Bills Deliverer
}

// DeliveryEffects returns the effect handler keyed by gate kind.
func DeliveryEffects(e *DeliveryEffect) map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{KindDelivery: e.Apply}
}

// Apply records the approved delivery. A bill already delivered on the approved date and
// channel is a no-op, so a repeat changes nothing; one delivered otherwise fails closed.
func (e *DeliveryEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	if err := checkDeliveryDeciders(g); err != nil {
		return err
	}
	if e == nil || e.Bills == nil {
		return fmt.Errorf("bill delivery (gate %s): not wired: %w", g.GateID, ErrInvalid)
	}
	var p deliveryPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil {
		return fmt.Errorf("bill delivery (gate %s): payload: %w", g.GateID, ErrInvalid)
	}
	ch, ok := deliveryChannels[p.Channel]
	on, err := time.ParseInLocation(time.DateOnly, p.DeliveredOn, business)
	switch {
	case p.BillID == "" || !ok || err != nil || p.EvidenceRef == "":
		return fmt.Errorf("bill delivery (gate %s): bill, channel, date and evidence are required: %w", g.GateID, ErrInvalid)
	case g.Subject.Kind == SubjectBill && g.Subject.ID != "" && g.Subject.ID != p.BillID:
		return fmt.Errorf("bill delivery (gate %s): subject is bill %s: %w", g.GateID, g.Subject.ID, ErrInvalid)
	}
	b, err := e.Bills.LockedBill(ctx, tx, ID(p.BillID))
	if err != nil {
		return fmt.Errorf("bill delivery (gate %s): %w", g.GateID, err)
	}
	// rule: ARC-I-012
	// Idempotent: the delivery this gate approved is already recorded.
	if !b.DeliveredAt.IsZero() {
		if businessDate(b.DeliveredAt).Equal(on) && b.DeliveryChannel == ch {
			return nil
		}
		return fmt.Errorf("bill delivery (gate %s): %w", g.GateID, ErrAlreadyDelivered)
	}
	// rule: BIL-L-003
	// Delivery is evidence from a channel on the approved business date, never in the future
	// (Deliver refuses that), after approval and signature.
	if _, err := e.Bills.Deliver(ctx, tx, ID(p.BillID), DeliveryEvidence{Channel: ch, AcceptedAt: on}); err != nil {
		return fmt.Errorf("bill delivery (gate %s): %w", g.GateID, err)
	}
	return nil
}

// checkDeliveryDeciders refuses no approver, an agent approver and the preparer (or the user
// an agent preparer acted for).
func checkDeliveryDeciders(g registry.Approved) error {
	if len(g.Approvers) == 0 {
		return fmt.Errorf("bill delivery (gate %s): no approver: %w", g.GateID, ErrInvalid)
	}
	prep := g.Preparer.UserID
	if g.Preparer.Kind == registry.PrincipalAgent {
		prep = g.Preparer.ActingFor
	}
	for _, a := range g.Approvers {
		// rule: ARC-I-019
		if a.Kind != registry.PrincipalUser || a.UserID == "" {
			return fmt.Errorf("bill delivery (gate %s): %w", g.GateID, ErrAgentApproves)
		}
		// rule: ARC-I-027
		if prep != "" && a.UserID == prep {
			return fmt.Errorf("bill delivery (gate %s): %w", g.GateID, ErrPreparerApproves)
		}
	}
	return nil
}
