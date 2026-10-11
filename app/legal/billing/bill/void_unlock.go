package bill

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/kernel/outbox"
)

// EventBillVoided is the outbox event a void emits (a2 events: bill.voided).
const EventBillVoided = "bill.voided"

// VoidedEvent is the bill.voided payload: ids only, never names, narratives or amounts.
type VoidedEvent struct {
	BillID       string   `json:"bill_id"`
	GateID       string   `json:"gate_id"`
	TimeEntryIDs []string `json:"time_entry_ids"`
	ExpenseIDs   []string `json:"expense_ids"`
}

// VoidPublisher is the VoidedSink that emits bill.voided in the void gate's tx, so the
// event commits or rolls back with the void.
type VoidPublisher struct {
	Outbox outbox.Emitter
}

var _ VoidedSink = VoidPublisher{}

// Voided emits bill.voided for plan.
func (p VoidPublisher) Voided(ctx context.Context, tx pgx.Tx, plan VoidPlan, gateID string) error {
	ev := VoidedEvent{
		BillID:       string(plan.Bill),
		GateID:       gateID,
		TimeEntryIDs: append([]string{}, plan.TimeEntryIDs...),
		ExpenseIDs:   append([]string{}, plan.ExpenseIDs...),
	}
	// rule: BIL-C-084
	// The void frees its entries: the time module returns them to approved on this event.
	if err := p.Outbox.Emit(ctx, tx, outbox.Subject{Kind: "bill", ID: ev.BillID}, EventBillVoided, ev); err != nil {
		return fmt.Errorf("emit %s for bill %s: %w", EventBillVoided, ev.BillID, err)
	}
	return nil
}

// TimeEntryReleaser is the time module's unlock port: a state-machine transition.
// timekeeping.Entries satisfies it.
type TimeEntryReleaser interface {
	Transition(ctx context.Context, tx pgx.Tx, id timekeeping.EntryID, to timekeeping.State, by timekeeping.Actor) error
}

// ReleaseVoidedEntries returns the time entries of a voided bill to approved through the
// time module's port, so they can go on a new bill. The bill module writes no time table.
// by is the user who decided the void.
func ReleaseVoidedEntries(ctx context.Context, tx pgx.Tx, port TimeEntryReleaser, ev VoidedEvent, by timekeeping.Actor) error {
	for _, id := range ev.TimeEntryIDs {
		// rule: BIL-C-084
		// Entries of a voided bill return to approved (ARC-I-050, CF-08).
		if err := port.Transition(ctx, tx, timekeeping.EntryID(id), timekeeping.StateApproved, by); err != nil {
			return fmt.Errorf("release entry %s of voided bill %s: %w", id, ev.BillID, err)
		}
	}
	return nil
}

// CheckTimeEntryFree says whether time entry e may go on a new draft bill. An entry is on
// at most one non-void bill: a void bill does not hold it (CF-08), any other bill does.
// e must also be approved in the time module.
func CheckTimeEntryFree(e timekeeping.TimeEntry, bills []Bill) error {
	for _, b := range bills {
		// rule: BIL-C-084
		// Uniqueness counts only non-void bills (CF-08).
		if b.Status == StatusVoid {
			continue
		}
		for _, l := range b.Lines {
			if l.Source == SourceTime && l.SourceID == string(e.ID) {
				return fmt.Errorf("entry %s on bill %s: %w", e.ID, b.ID, timekeeping.ErrBilledLocked)
			}
		}
	}
	if e.State != timekeeping.StateApproved {
		return fmt.Errorf("entry %s in %s: %w", e.ID, e.State, timekeeping.ErrBadTransition)
	}
	return nil
}
