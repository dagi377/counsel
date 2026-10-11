package bill

import (
	"fmt"
	"strings"
)

// POSAction is how a place-of-supply change is carried out on a bill.
type POSAction string

// Place-of-supply change actions.
const (
	// POSEditDraft edits a bill that is not yet approved in place.
	POSEditDraft POSAction = "edit_draft"
	// POSVoidRebuild voids an approved, undelivered bill and rebuilds it under a new number.
	POSVoidRebuild POSAction = "void_rebuild"
	// POSCreditRebill issues a full credit note on a delivered bill and a new bill.
	POSCreditRebill POSAction = "credit_rebill"
)

// POSPlan is what a place-of-supply change does. The original bill is never moved back to
// draft: after approval it is voided or fully credited, and the change lands on Rebuild.
type POSPlan struct {
	Action      POSAction
	Original    ID
	CreditCents int64 // full bill total credited; set only for POSCreditRebill
	Rebuild     Bill  // the new draft bill; zero for POSEditDraft
}

// PlanPlaceOfSupplyChange decides how to move bill b to place of supply province.
// It returns ErrInvalid for an empty or unchanged province and ErrBillGuard for a bill
// that is void, credited or written off.
func PlanPlaceOfSupplyChange(b Bill, province string) (POSPlan, error) {
	province = strings.TrimSpace(province)
	if province == "" || province == b.PlaceOfSupplyProvince {
		return POSPlan{}, fmt.Errorf("place of supply change on bill %s: %w", b.ID, ErrInvalid)
	}
	switch b.Status {
	case StatusDraft, StatusPreflightFailed, StatusPendingApproval:
		return POSPlan{Action: POSEditDraft, Original: b.ID}, nil
	case StatusVoid, StatusCredited, StatusWrittenOff:
		return POSPlan{}, fmt.Errorf("place of supply change on bill %s in %s: %w", b.ID, b.Status, ErrBillGuard)
	}
	// rule: BIL-C-207
	// From approval on the bill is frozen. Before delivery it is voided and rebuilt with a
	// new number; after delivery it gets a full credit note and a new bill. It never
	// returns to draft.
	plan := POSPlan{Original: b.ID, Rebuild: rebuild(b, province)}
	if b.DeliveredAt.IsZero() {
		plan.Action = POSVoidRebuild
		return plan, nil
	}
	plan.Action = POSCreditRebill
	plan.CreditCents = b.TotalCents
	return plan, nil
}

// rebuild returns a new draft copy of b with the new place of supply and no approval,
// number, signature or delivery. Tax is recomputed when the draft is previewed.
func rebuild(b Bill, province string) Bill {
	lines := make([]Line, len(b.Lines))
	copy(lines, b.Lines)
	return Bill{
		MatterID:              b.MatterID,
		PayerID:               b.PayerID,
		Status:                StatusDraft,
		DueDate:               b.DueDate,
		Lines:                 lines,
		FeesCents:             b.FeesCents,
		TaxableDisbCents:      b.TaxableDisbCents,
		NontaxableDisbCents:   b.NontaxableDisbCents,
		DiscountCents:         b.DiscountCents,
		InterestRatePPM:       b.InterestRatePPM,
		PlaceOfSupplyProvince: province,
		Paralegal:             b.Paralegal,
	}
}
