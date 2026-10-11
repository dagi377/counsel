package bill

import (
	"errors"
	"testing"
	"time"
)

// rule: BIL-C-207
func TestBIL_C_207_PlaceOfSupplyChange(t *testing.T) {
	delivered := time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)
	base := Bill{
		ID: "b1", Number: "INV-2026-0001", MatterID: "m1", PayerID: "p1",
		Lines:     []Line{{Ordinal: 1, Source: SourceTime, AmountCents: 80000, TaxCode: "HST-ON"}},
		FeesCents: 80000, TaxCents: 10400, TotalCents: 90400,
		PlaceOfSupplyProvince: "ON", ApprovalID: "g1", ApprovedBy: "u2", SignedBy: "u2",
	}
	with := func(s Status, d time.Time) Bill { b := base; b.Status = s; b.DeliveredAt = d; return b }
	tests := []struct {
		name     string
		bill     Bill
		province string
		action   POSAction
		credit   int64
		err      error
	}{
		{"draft edits in place", with(StatusDraft, time.Time{}), "AB", POSEditDraft, 0, nil},
		{"approved undelivered voids and rebuilds", with(StatusApproved, time.Time{}), "AB", POSVoidRebuild, 0, nil},
		{"delivered gets full credit and new bill", with(StatusDelivered, delivered), "AB", POSCreditRebill, 90400, nil},
		{"partially paid gets full credit and new bill", with(StatusPartiallyPaid, delivered), "AB", POSCreditRebill, 90400, nil},
		{"paid gets full credit and new bill", with(StatusPaid, delivered), "AB", POSCreditRebill, 90400, nil},
		{"void bill is frozen", with(StatusVoid, time.Time{}), "AB", "", 0, ErrBillGuard},
		{"credited bill is frozen", with(StatusCredited, delivered), "AB", "", 0, ErrBillGuard},
		{"same province is invalid", with(StatusDelivered, delivered), "ON", "", 0, ErrInvalid},
		{"empty province is invalid", with(StatusDelivered, delivered), " ", "", 0, ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := tc.bill
			p, err := PlanPlaceOfSupplyChange(tc.bill, tc.province)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if err != nil {
				return
			}
			if p.Action != tc.action || p.CreditCents != tc.credit || p.Original != tc.bill.ID {
				t.Fatalf("plan = %+v, want action %s credit %d", p, tc.action, tc.credit)
			}
			if tc.bill.Status != orig.Status {
				t.Fatalf("original bill status changed to %s", tc.bill.Status)
			}
			if p.Action == POSEditDraft {
				return
			}
			r := p.Rebuild
			if r.Status != StatusDraft || r.ID != "" || r.Number != "" || r.ApprovalID != "" ||
				r.SignedBy != "" || !r.DeliveredAt.IsZero() {
				t.Fatalf("rebuild carries frozen state: %+v", r)
			}
			if r.PlaceOfSupplyProvince != tc.province || r.MatterID != "m1" || len(r.Lines) != 1 {
				t.Fatalf("rebuild = %+v", r)
			}
		})
	}
}
