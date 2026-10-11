package batch

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// EventCycleRequested is published when a user asks for a billing cycle (a batch run of
// draft bills up to a cut-off). F-BIL-01 starts on it.
const EventCycleRequested = "billing.cycle_requested"

// SubjectCycle is the outbox subject kind of a billing cycle; its id is the batch run id.
const SubjectCycle = "billing.cycle"

// CycleRequested is the body of EventCycleRequested: ids and the cut-off day only.
type CycleRequested struct {
	RunID    string `json:"batch_run_id"`
	PeriodTo string `json:"period_to"` // YYYY-MM-DD, America/Toronto
	UserID   string `json:"user_id"`
}

// PublishCycleRequested publishes EventCycleRequested in tx (nil: the emitter opens its
// own). Minimal publisher: main has no cycle-request write point yet (Q-EVENTS-7).
func PublishCycleRequested(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b CycleRequested) error {
	if b.RunID == "" || b.PeriodTo == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventCycleRequested, ErrInvalid)
	}
	if em == nil {
		return nil
	}
	// rule: ARC-I-030
	if err := em.Emit(ctx, tx, outbox.Subject{Kind: SubjectCycle, ID: b.RunID}, EventCycleRequested, b); err != nil {
		return fmt.Errorf("publish %s: %w", EventCycleRequested, err)
	}
	return nil
}
