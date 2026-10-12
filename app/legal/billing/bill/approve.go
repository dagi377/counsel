package bill

import (
	"fmt"
	"slices"
	"time"
	_ "time/tzdata" // the business zone must resolve on hosts without a zoneinfo database

	"github.com/dagi377/counsel/kernel/clock"
)

// business is the zone bill dates are taken in.
var business = mustZone(clock.DefaultZone)

func mustZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(fmt.Sprintf("bill: load zone %s: %v", name, err))
	}
	return loc
}

// businessDate is t's calendar date in America/Toronto, at midnight there.
func businessDate(t time.Time) time.Time {
	return clock.DateOf(t, business).Start(business)
}

// editable reports whether a bill in status s may still be edited.
func editable(s Status) bool {
	return s == StatusDraft || s == StatusPreflightFailed || s == StatusPendingApproval
}

// applyApproval returns b approved under a with number, which the caller took from
// NextNumber in the same tx. It has no I/O. Replaying an approval already applied returns
// b unchanged. Delivery fields are never touched: only Deliver sets them.
// sole says gates.CheckSoD already allowed the approver to approve their own bill as the
// firm's sole practitioner (step-up, stated reason, audited).
func applyApproval(b Bill, a Approval, number string, sole bool) (Bill, error) {
	if a.BillID != b.ID || a.ApprovalID == "" || a.ApprovedBy == "" || a.ApprovedAt.IsZero() {
		return Bill{}, fmt.Errorf("approve %s: %w", b.ID, ErrInvalid)
	}
	if b.ApprovalID == a.ApprovalID && b.Status != StatusPendingApproval {
		b.Lines = slices.Clone(b.Lines)
		return b, nil
	}
	if b.Status != StatusPendingApproval {
		return Bill{}, fmt.Errorf("approve %s in %s: %w", b.ID, b.Status, ErrNotPending)
	}
	// rule: ARC-I-021
	// The person who prepares a bill never approves it, except a sole practitioner.
	if a.ApprovedBy == b.PreparedBy && !sole {
		return Bill{}, fmt.Errorf("approve %s: %w", b.ID, ErrPreparerApproves)
	}
	// rule: BIL-C-121
	// The bill date is the approval date in America/Toronto; HST is reported in its period.
	issue := businessDate(a.ApprovedAt)
	prefix, year, _, err := ParseNumber(number)
	if err != nil || prefix != a.Prefix || year != issue.Year() {
		return Bill{}, fmt.Errorf("approve %s: number %q for %s-%04d: %w", b.ID, number, a.Prefix, issue.Year(), ErrInvalid)
	}
	b.Lines = slices.Clone(b.Lines)
	b.Status = StatusApproved
	b.Number = number
	b.IssueDate = issue
	b.ApprovalID, b.ApprovedBy, b.ApprovedAt = a.ApprovalID, a.ApprovedBy, a.ApprovedAt
	// rule: BIL-L-003
	// Approval is not delivery: DeliveredAt and DeliveredOn stay as they were (zero).
	return b, nil
}

// applyChange returns b with c applied. It has no I/O. DeliveredAt is read-only here.
// An approved bill is frozen: a discount or line change returns ErrBillGuard (409
// bill_immutable); a discount also matches ErrUseCreditNote, since a reduction after
// approval is a credit note.
func applyChange(b Bill, c Change) (Bill, error) {
	if c.DeliveredAt != nil {
		return Bill{}, fmt.Errorf("update %s: delivered_at: %w", b.ID, ErrReadOnlyField)
	}
	// rule: BIL-C-024
	if !editable(b.Status) {
		if c.DiscountCents != nil {
			return Bill{}, fmt.Errorf("update %s in %s: discount: %w: %w", b.ID, b.Status, ErrBillGuard, ErrUseCreditNote)
		}
		return Bill{}, fmt.Errorf("update %s in %s: %w", b.ID, b.Status, ErrBillGuard)
	}
	b.Lines = slices.Clone(b.Lines)
	if c.DiscountCents != nil {
		d := *c.DiscountCents
		if d < 0 || d > b.FeesCents+b.TaxableDisbCents {
			return Bill{}, fmt.Errorf("update %s: discount: %w", b.ID, ErrInvalid)
		}
		b.TotalCents += b.DiscountCents - d
		b.DiscountCents = d
	}
	if c.DueDate != nil {
		b.DueDate = *c.DueDate
	}
	if c.InterestRatePPM != nil {
		r := *c.InterestRatePPM
		b.InterestRatePPM = &r
	}
	if c.Lines != nil {
		b.Lines = slices.Clone(c.Lines)
	}
	return b, nil
}
