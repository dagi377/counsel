package bill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// MessageDelivered is the outbox message a delivery channel emits when it has evidence that a
// bill reached the payer (or bounced).
const MessageDelivered = "billing.bill.delivered"

// ConsumerDeliver is the outbox consumer name, and so the inbox key, of the delivery consumer.
const ConsumerDeliver = "billing.bill_deliver"

// DeliveredBody is the payload of a MessageDelivered message: ids and times only.
type DeliveredBody struct {
	BillID     string    `json:"bill_id"`
	Channel    Channel   `json:"channel"`
	AcceptedAt time.Time `json:"accepted_at"`
	Bounced    bool      `json:"bounced,omitempty"`
}

// DeliverConsumer records delivery evidence through s.Deliver for every MessageDelivered
// message, in the relay's tx. The kernel outbox.relay River job runs it and claims (consumer,
// message id) in the same tx, so a redelivered message is a no-op. Other message types are
// skipped.
func DeliverConsumer(s Service) outbox.Consumer {
	return outbox.Consumer{Name: ConsumerDeliver, Handle: func(ctx context.Context, tx pgx.Tx, m outbox.Message) error {
		if m.Type != MessageDelivered {
			return nil
		}
		var b DeliveredBody
		if err := json.Unmarshal(m.Payload, &b); err != nil || b.BillID == "" {
			return fmt.Errorf("deliver message %s: %w", m.ID, ErrInvalid)
		}
		// rule: BIL-L-003
		// delivered_at is set only from channel evidence, once.
		_, err := s.Deliver(ctx, tx, ID(b.BillID), DeliveryEvidence{Channel: b.Channel, AcceptedAt: b.AcceptedAt, Bounced: b.Bounced})
		if errors.Is(err, ErrAlreadyDelivered) {
			return nil // a second message for a delivered bill changes nothing
		}
		if err != nil {
			return fmt.Errorf("deliver message %s: %w", m.ID, err)
		}
		return nil
	}}
}
