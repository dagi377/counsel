// Minimal publishers for the kernel events this package's agent features bind to but no
// write path publishes yet (EVENTS-9, docs/QUESTIONS.md Q-EVENTS-9). The owning write path
// calls its publisher in the same transaction when it lands. Bodies carry ids, days, counts
// and codes only, plus user_id: the user an event run acts for (trigger.PayloadActor).

package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// ErrEventBody means an event body lacks a required id; nothing is published.
var ErrEventBody = errors.New("billing: event body missing a required field")

// GateDecided is the body of EventGateDecided: a gate decision was recorded.
type GateDecided struct {
	GateID    string `json:"gate_id"`
	Version   int64  `json:"version"`
	Decision  string `json:"decision"`
	DeciderID string `json:"decider_id"`
	Final     bool   `json:"final"`
	UserID    string `json:"user_id"`
}

// PublishGateDecided publishes EventGateDecided in tx (nil: the emitter opens its own).
func PublishGateDecided(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b GateDecided) error {
	if b.GateID == "" || b.Decision == "" || b.DeciderID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventGateDecided, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "gate", ID: b.GateID}, EventGateDecided, b)
}

// PaymentReceived is the body of EventPaymentReceived: a payment was received.
type PaymentReceived struct {
	PaymentID   string `json:"payment_id"`
	AmountCents int64  `json:"amount_cents"`
	Method      string `json:"method"`
	Account     string `json:"account"`
	ProviderRef string `json:"provider_ref,omitempty"`
	UserID      string `json:"user_id"`
}

// PublishPaymentReceived publishes EventPaymentReceived in tx (nil: the emitter opens its own).
func PublishPaymentReceived(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b PaymentReceived) error {
	if b.PaymentID == "" || b.Method == "" || b.Account == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventPaymentReceived, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "payment", ID: b.PaymentID}, EventPaymentReceived, b)
}

// publishEvent emits one event; a nil emitter publishes nothing.
func publishEvent(ctx context.Context, em outbox.Emitter, tx pgx.Tx, s outbox.Subject, typ string, body any) error {
	if em == nil {
		return nil
	}
	// rule: ARC-I-030
	if err := em.Emit(ctx, tx, s, typ, body); err != nil {
		return fmt.Errorf("publish %s: %w", typ, err)
	}
	return nil
}
