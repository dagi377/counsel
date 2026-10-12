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
	// NoReliefReason says why TaxCents is zero when relief was refused, e.g. the client
	// does not deal at arm's length; empty when relief was computed.
	NoReliefReason string
}

// NoReliefNotArmsLength is the recorded reason when ETA s.231 relief is refused because the
// client does not deal with the firm at arm's length.
const NoReliefNotArmsLength = "ETA s.231: client not at arm's length; no bad-debt relief"

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
	if w.AmountCents <= 0 || w.InterestCents < 0 || w.InterestCents > w.AmountCents || on.IsZero() {
		return WriteOff{}, BadDebtAdjustment{}, fmt.Errorf("approve write-off %s: %w", w.ID, ErrInvalidAmount)
	}
	if w.AmountCents > b.OpenCents {
		return WriteOff{}, BadDebtAdjustment{}, fmt.Errorf("approve write-off %s: %w", w.ID, ErrExceedsBalance)
	}
	// rule: BIL-T-080
	// ETA s.231 relief needs an arm's-length determination recorded on the write-off.
	// A write-off with none fails closed rather than claim relief by default.
	if w.ArmsLength != ArmsLengthYes && w.ArmsLength != ArmsLengthNo {
		return WriteOff{}, BadDebtAdjustment{}, fmt.Errorf("approve write-off %s: %w", w.ID, ErrArmsLengthUnchecked)
	}
	// rule: BIL-C-205
	// rule: BIL-T-080
	// The adjustment is the bill's open tax on the written-off consideration,
	// rhu(bill_tax * (written_off - interest), bill_total), posted in the period of the
	// write-off. ETA s.231 relieves only the tax fraction of the consideration for the
	// taxable supply; interest is not that consideration, so it carries no relief.
	var tax int64
	var noRelief string
	if w.ArmsLength == ArmsLengthNo {
		// rule: BIL-T-080
		// Not at arm's length: no relief, and the reason is recorded on the adjustment.
		noRelief = NoReliefNotArmsLength
	} else if taxable := w.AmountCents - w.InterestCents; taxable > 0 {
		t, err := c.BadDebtTax(taxable, billTax, billTotal)
		if err != nil {
			return WriteOff{}, BadDebtAdjustment{}, fmt.Errorf("approve write-off %s: %w", w.ID, err)
		}
		tax = t
	}
	w.Status = StatusApproved
	w.ApprovedBy = by.UserID
	at := on
	w.ApprovedAt = &at
	return w, BadDebtAdjustment{BillID: b.ID, TaxCents: tax, On: on, NoReliefReason: noRelief}, nil
}
