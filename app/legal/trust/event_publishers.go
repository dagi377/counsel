// Minimal publishers for the kernel events this package's agent features bind to but no
// write path publishes yet (EVENTS-9, docs/QUESTIONS.md Q-EVENTS-9). The owning write path
// calls its publisher in the same transaction when it lands. Bodies carry ids, days, counts
// and codes only, plus user_id: the user an event run acts for (trigger.PayloadActor).

package trust

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// ErrEventBody means an event body lacks a required id; nothing is published.
var ErrEventBody = errors.New("trust: event body missing a required field")

// LedgerBelowFloor is the body of EventLedgerBelowFloor: a matter's trust ledger fell below its retainer floor (no amount).
type LedgerBelowFloor struct {
	MatterID string `json:"matter_id"`
	UserID   string `json:"user_id"`
}

// PublishLedgerBelowFloor publishes EventLedgerBelowFloor in tx (nil: the emitter opens its own).
func PublishLedgerBelowFloor(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b LedgerBelowFloor) error {
	if b.MatterID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventLedgerBelowFloor, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "matter", ID: b.MatterID}, EventLedgerBelowFloor, b)
}

// StatementReceived is the body of EventStatementReceived: a trust bank statement was received.
type StatementReceived struct {
	StatementID string `json:"statement_id"`
	AccountID   string `json:"account_id"`
	UserID      string `json:"user_id"`
}

// PublishStatementReceived publishes EventStatementReceived in tx (nil: the emitter opens its own).
func PublishStatementReceived(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b StatementReceived) error {
	if b.StatementID == "" || b.AccountID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventStatementReceived, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "trust_statement", ID: b.StatementID}, EventStatementReceived, b)
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
