package bill

import (
	"fmt"
	"time"
)

// ProblemDeliveredUseCreditNote is the problem+json code for a void of a delivered bill.
// The handler maps ErrUseCreditNote from planVoid to 409 with this code.
const ProblemDeliveredUseCreditNote = "bill_delivered_use_credit_note"

// VoidAction is how a void request is carried out on a bill.
type VoidAction string

// Void actions.
const (
	// VoidDiscard drops a bill that was never approved. It has no number and no journal entry.
	VoidDiscard VoidAction = "discard"
	// VoidReverse voids an approved, undelivered bill: the number stays, the approval entry
	// is reversed on the void date and the bill's entries become billable again.
	VoidReverse VoidAction = "reverse"
)

// Voider is who decides a void through the bill void gate.
type Voider struct {
	UserID  string
	IsAgent bool
	// SoleCleared is true when gates.CheckSoD allowed the voider to decide their own bill's
	// void as the firm's sole practitioner (step-up, stated reason, audited).
	SoleCleared bool
}

// VoidPlan is what a void does to one bill. A void never creates a credit note.
type VoidPlan struct {
	Action VoidAction
	Bill   ID
	// Number is the bill number, kept on a voided bill; empty for VoidDiscard.
	Number string
	// ReverseOn is the business date (America/Toronto) of the reversal of the approval
	// entry; zero for VoidDiscard.
	ReverseOn time.Time
	// TimeEntryIDs and ExpenseIDs are the entries returned to approved and unbilled.
	TimeEntryIDs []string
	ExpenseIDs   []string
	// Status is the bill status after the void.
	Status Status
}

// planVoid decides how bill b is voided by by on business date on. It does no I/O.
// A draft or pending bill is discarded. An approved, undelivered bill keeps its number,
// reverses its approval entry on on and frees its entries. A delivered bill returns
// ErrUseCreditNote; a void by the preparer or an agent returns ErrPreparerApproves.
func planVoid(b Bill, by Voider, on time.Time) (VoidPlan, error) {
	switch b.Status {
	case StatusDraft, StatusPreflightFailed, StatusPendingApproval:
		// rule: BIL-C-084
		// A bill that was never approved has no number and no journal entry: discard it.
		return VoidPlan{Action: VoidDiscard, Bill: b.ID, Status: StatusVoid}, nil
	case StatusVoid, StatusCredited, StatusWrittenOff:
		return VoidPlan{}, fmt.Errorf("void bill %s in %s: %w", b.ID, b.Status, ErrBillGuard)
	}
	// rule: ARC-I-050
	// Void only before delivery; a delivered bill is corrected by a credit note (CF-20).
	if !b.DeliveredAt.IsZero() || b.Status != StatusApproved {
		return VoidPlan{}, fmt.Errorf("void bill %s: %w", b.ID, ErrUseCreditNote)
	}
	// rule: BIL-C-084
	// The void is decided under G-B2: never by the preparer, never by an agent.
	// rule: ARC-I-021
	if by.IsAgent || by.UserID == "" || (by.UserID == b.PreparedBy && !by.SoleCleared) {
		return VoidPlan{}, fmt.Errorf("void bill %s: %w", b.ID, ErrPreparerApproves)
	}
	if on.IsZero() || (!b.IssueDate.IsZero() && on.Before(b.IssueDate)) {
		return VoidPlan{}, fmt.Errorf("void bill %s: void date: %w", b.ID, ErrInvalid)
	}
	// rule: BIL-C-084
	// The number is kept, the approval entry is reversed on the void date and the bill's
	// entries return to approved. No credit note is planned.
	plan := VoidPlan{
		Action:    VoidReverse,
		Bill:      b.ID,
		Number:    b.Number,
		ReverseOn: on,
		Status:    StatusVoid,
	}
	for _, l := range b.Lines {
		if l.SourceID == "" {
			continue
		}
		switch l.Source {
		case SourceTime:
			plan.TimeEntryIDs = append(plan.TimeEntryIDs, l.SourceID)
		case SourceExpense:
			plan.ExpenseIDs = append(plan.ExpenseIDs, l.SourceID)
		}
	}
	return plan, nil
}
