package credit

import (
	"errors"
	"fmt"
	"time"
)

// GateWriteOff is the gate a write-off is approved through.
const GateWriteOff = "G-B5"

// ErrNotResponsible: the approver of a write-off is not the bill's responsible lawyer.
var ErrNotResponsible = errors.New("credit: approver is not the responsible lawyer")

// BadDebtAdjustment is the tax adjustment a write-off posts, in the period of the write-off.
type BadDebtAdjustment struct {
	BillID   string
	TaxCents int64
	On       time.Time // business date of the write-off, America/Toronto
}

// CheckWriteOffApprover rejects an approval of w on bill b through G-B5 by anyone but the
// bill's responsible lawyer, by the preparer, or by an agent.
func CheckWriteOffApprover(w WriteOff, b Bill, by Approver) error {
	// rule: BIL-C-205
	// A write-off is approved through G-B5 by the responsible lawyer, never by the
	// requester and never by an agent.
	switch {
	case by.IsAgent:
		return fmt.Errorf("approve write-off %s: %w", w.ID, ErrAgentApproval)
	case by.UserID == "" || by.UserID == w.PreparedBy:
		return fmt.Errorf("approve write-off %s: %w", w.ID, ErrSelfApproval)
	case by.UserID != b.ResponsibleUser:
		return fmt.Errorf("approve write-off %s: %w", w.ID, ErrNotResponsible)
	}
	return nil
}

// ApproveWriteOff approves draft w on bill b on business date on. billTax and billTotal are
// the bill's issued tax and total including tax. It returns the approved write-off and the
// bad-debt tax adjustment for its open tax.
func (c Calc) ApproveWriteOff(w WriteOff, b Bill, billTax, billTotal int64, by Approver, on time.Time) (WriteOff, BadDebtAdjustment, error) {
	if w.Status != StatusDraft || w.BillID != b.ID {
		return WriteOff{}, BadDebtAdjustment{}, fmt.Errorf("approve write-off %s: %w", w.ID, ErrInvalidState)
	}
	if err := CheckDelivered(b); err != nil {
		return WriteOff{}, BadDebtAdjustment{}, err
	}
	if err := CheckWriteOffApprover(w, b, by); err != nil {
		return WriteOff{}, BadDebtAdjustment{}, err
	}
	if w.AmountCents <= 0 || on.IsZero() {
		return WriteOff{}, BadDebtAdjustment{}, fmt.Errorf("approve write-off %s: %w", w.ID, ErrInvalidAmount)
	}
	if w.AmountCents > b.OpenCents {
		return WriteOff{}, BadDebtAdjustment{}, fmt.Errorf("approve write-off %s: %w", w.ID, ErrExceedsBalance)
	}
	// rule: BIL-C-205
	// The adjustment is the bill's open tax on the written-off amount,
	// rhu(bill_tax * written_off, bill_total), posted in the period of the write-off.
	tax, err := c.BadDebtTax(w.AmountCents, billTax, billTotal)
	if err != nil {
		return WriteOff{}, BadDebtAdjustment{}, fmt.Errorf("approve write-off %s: %w", w.ID, err)
	}
	w.Status = StatusApproved
	w.ApprovedBy = by.UserID
	at := on
	w.ApprovedAt = &at
	return w, BadDebtAdjustment{BillID: b.ID, TaxCents: tax, On: on}, nil
}
