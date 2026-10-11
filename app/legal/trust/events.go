package trust

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// Trust events (spec/eng/a2-interfaces.json, EVENTS-8). Bodies carry ids, dates and integer
// cents only.
const (
	EventStatementReceived = "trust.statement_received"
	EventLedgerBelowFloor  = "trust.ledger_below_floor"
)

// ErrEventInvalid means a trust event body is missing a field; ErrNoEventEmitter means the
// publisher has no emitter, so the event would be lost.
var (
	ErrEventInvalid   = errors.New("trust: invalid event")
	ErrNoEventEmitter = errors.New("trust: no event emitter")
)

// StatementReceived is the trust.statement_received body: a bank statement stored for an
// account and period (dates YYYY-MM-DD, America/Toronto).
type StatementReceived struct {
	StatementID string `json:"statement_id"`
	AccountID   string `json:"account_id"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
}

// LedgerBelowFloor is the trust.ledger_below_floor body: a client ledger whose balance fell
// below the matter's floor (a retainer replenishment threshold).
type LedgerBelowFloor struct {
	LedgerID     string `json:"ledger_id"`
	MatterID     string `json:"matter_id"`
	BalanceCents int64  `json:"balance_cents"`
	FloorCents   int64  `json:"floor_cents"`
}

// Events publishes trust events in the caller's transaction. Statement import and the floor
// check have no write point yet; they call it when they store a statement or post below the
// floor (Q-EVENTS-8).
type Events struct {
	Emitter outbox.Emitter
}

// StatementReceived publishes trust.statement_received.
func (e Events) StatementReceived(ctx context.Context, tx pgx.Tx, b StatementReceived) error {
	ok := b.StatementID != "" && b.AccountID != "" && b.PeriodStart != "" && b.PeriodEnd != ""
	return e.emit(ctx, tx, "bank_statement", b.StatementID, EventStatementReceived, b, ok)
}

// LedgerBelowFloor publishes trust.ledger_below_floor. A balance at or above the floor is
// refused: nothing fell below it.
func (e Events) LedgerBelowFloor(ctx context.Context, tx pgx.Tx, b LedgerBelowFloor) error {
	ok := b.LedgerID != "" && b.MatterID != "" && b.FloorCents >= 0 && b.BalanceCents < b.FloorCents
	return e.emit(ctx, tx, "trust_ledger", b.LedgerID, EventLedgerBelowFloor, b, ok)
}

func (e Events) emit(ctx context.Context, tx pgx.Tx, kind, id, typ string, body any, ok bool) error {
	if !ok {
		return fmt.Errorf("publish %s: %w", typ, ErrEventInvalid)
	}
	if e.Emitter == nil {
		return fmt.Errorf("publish %s: %w", typ, ErrNoEventEmitter)
	}
	if err := e.Emitter.Emit(ctx, tx, outbox.Subject{Kind: kind, ID: id}, typ, body); err != nil {
		return fmt.Errorf("publish %s: %w", typ, err)
	}
	return nil
}
