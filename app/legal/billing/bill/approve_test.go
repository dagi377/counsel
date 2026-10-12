package bill

import (
	"errors"
	"testing"
	"time"
)

func pendingBill() Bill {
	return Bill{
		ID: "b1", MatterID: "m1", PayerID: "p1", Status: StatusPendingApproval, PreparedBy: "u1",
		Lines:     []Line{{Ordinal: 1, Source: SourceTime, AmountCents: 100000, TaxCode: "HST-ON"}},
		FeesCents: 100000, TaxCents: 13000, TotalCents: 113000, PlaceOfSupplyProvince: "ON",
	}
}

func approvalAt(at time.Time) Approval {
	return Approval{BillID: "b1", ApprovalID: "g1", ApprovedBy: "u2", ApprovedAt: at, Prefix: "INV"}
}

func TestApplyApproval(t *testing.T) {
	at := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	approved, err := applyApproval(pendingBill(), approvalAt(at), "INV-2026-0147", false)
	if err != nil {
		t.Fatal(err)
	}
	with := func(f func(*Bill)) Bill { b := pendingBill(); f(&b); return b }
	withA := func(f func(*Approval)) Approval { a := approvalAt(at); f(&a); return a }
	tests := []struct {
		name   string
		bill   Bill
		a      Approval
		number string
		want   string // expected number after approval
		err    error
	}{
		{"pending approves", pendingBill(), approvalAt(at), "INV-2026-0147", "INV-2026-0147", nil},
		// rule: BIL-T-015
		// A zero-rated export (draft.ExportProvince, no tax) approves like any other bill.
		{"zero-rated export approves", with(func(b *Bill) { b.TaxCents, b.TotalCents, b.PlaceOfSupplyProvince = 0, 100000, "EX" }),
			approvalAt(at), "INV-2026-0147", "INV-2026-0147", nil},
		{"replay is a no-op", approved, approvalAt(at), "INV-2026-0148", "INV-2026-0147", nil},
		// The person who prepares an item never approves it.
		{"preparer may not approve", pendingBill(), withA(func(a *Approval) { a.ApprovedBy = "u1" }), "INV-2026-0147", "", ErrPreparerApproves},
		{"draft is not pending", with(func(b *Bill) { b.Status = StatusDraft }), approvalAt(at), "INV-2026-0147", "", ErrNotPending},
		{"other approval on approved bill", approved, withA(func(a *Approval) { a.ApprovalID = "g2" }), "INV-2026-0148", "", ErrNotPending},
		{"void is not pending", with(func(b *Bill) { b.Status = StatusVoid }), approvalAt(at), "INV-2026-0147", "", ErrNotPending},
		{"missing approval id", pendingBill(), withA(func(a *Approval) { a.ApprovalID = "" }), "INV-2026-0147", "", ErrInvalid},
		{"missing approver", pendingBill(), withA(func(a *Approval) { a.ApprovedBy = "" }), "INV-2026-0147", "", ErrInvalid},
		{"zero approval time", pendingBill(), withA(func(a *Approval) { a.ApprovedAt = time.Time{} }), "INV-2026-0147", "", ErrInvalid},
		{"other bill", pendingBill(), withA(func(a *Approval) { a.BillID = "b2" }), "INV-2026-0147", "", ErrInvalid},
		{"malformed number", pendingBill(), approvalAt(at), "INV-2026-147", "", ErrInvalid},
		{"number of other prefix", pendingBill(), approvalAt(at), "CRN-2026-0147", "", ErrInvalid},
		{"number of other year", pendingBill(), approvalAt(at), "INV-2025-0147", "", ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyApproval(tc.bill, tc.a, tc.number, false)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err != nil {
				return
			}
			if got.Status != StatusApproved || got.Number != tc.want || got.ApprovalID != "g1" ||
				got.ApprovedBy != "u2" || !got.ApprovedAt.Equal(at) {
				t.Fatalf("approved bill = %+v", got)
			}
		})
	}
}

// rule: BIL-C-121
func TestBIL_C_121_BillDateIsApprovalDate(t *testing.T) {
	tests := []struct {
		name       string
		approvedAt time.Time
		number     string
		wantDate   string
	}{
		// 2026-09-30 23:30 Toronto (EDT) is 2026-10-01 03:30 UTC; the bill is still September.
		{"late evening Toronto stays September", time.Date(2026, 10, 1, 3, 30, 0, 0, time.UTC), "INV-2026-0147", "2026-09-30"},
		{"just after midnight Toronto is October", time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC), "INV-2026-0147", "2026-10-01"},
		// 2026-12-31 22:00 Toronto (EST) is 2027-01-01 03:00 UTC: bill year and number are 2026.
		{"new year in UTC is still old year in Toronto", time.Date(2027, 1, 1, 3, 0, 0, 0, time.UTC), "INV-2026-0147", "2026-12-31"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := applyApproval(pendingBill(), approvalAt(tc.approvedAt), tc.number, false)
			if err != nil {
				t.Fatal(err)
			}
			if got := b.IssueDate.In(business).Format("2006-01-02"); got != tc.wantDate {
				t.Fatalf("issue date = %s, want %s", got, tc.wantDate)
			}
			if h, m, s := b.IssueDate.In(business).Clock(); h+m+s != 0 {
				t.Fatalf("issue date %v is not midnight Toronto", b.IssueDate)
			}
		})
	}
	// Delivered 2026-10-01 does not move the bill date (and so the HST period) to October.
	b, err := applyApproval(pendingBill(), approvalAt(time.Date(2026, 10, 1, 3, 30, 0, 0, time.UTC)), "INV-2026-0147", false)
	if err != nil {
		t.Fatal(err)
	}
	b.DeliveredAt = time.Date(2026, 10, 1, 14, 0, 0, 0, time.UTC)
	if b.IssueDate.In(business).Month() != time.September {
		t.Fatalf("issue date %v moved off September", b.IssueDate)
	}
}

// rule: BIL-C-024
func TestBIL_C_024_NoDiscountAfterApproval(t *testing.T) {
	disc := int64(5000)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, business)
	lines := []Line{{Ordinal: 1, Source: SourceTime, AmountCents: 90000}}
	tests := []struct {
		name   string
		status Status
		c      Change
		err    error
		credit bool // error also matches ErrUseCreditNote
	}{
		{"discount on approved", StatusApproved, Change{DiscountCents: &disc}, ErrBillGuard, true},
		{"discount on delivered", StatusDelivered, Change{DiscountCents: &disc}, ErrBillGuard, true},
		{"lines on approved", StatusApproved, Change{Lines: lines}, ErrBillGuard, false},
		{"empty lines on approved", StatusApproved, Change{Lines: []Line{}}, ErrBillGuard, false},
		{"due date on approved", StatusApproved, Change{DueDate: &due}, ErrBillGuard, false},
		{"lines on void", StatusVoid, Change{Lines: lines}, ErrBillGuard, false},
		{"discount on draft", StatusDraft, Change{DiscountCents: &disc}, nil, false},
		{"lines on pending", StatusPendingApproval, Change{Lines: lines}, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := pendingBill()
			b.Status = tc.status
			got, err := applyChange(b, tc.c)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if errors.Is(err, ErrUseCreditNote) != tc.credit {
				t.Fatalf("err = %v, ErrUseCreditNote match want %v", err, tc.credit)
			}
			if tc.err != nil {
				return
			}
			if tc.c.DiscountCents != nil && (got.DiscountCents != disc || got.TotalCents != 108000) {
				t.Fatalf("discount not applied: %+v", got)
			}
			if tc.c.Lines != nil && (len(got.Lines) != 1 || got.Lines[0].AmountCents != 90000) {
				t.Fatalf("lines not applied: %+v", got.Lines)
			}
		})
	}
}

// rule: BIL-L-003
func TestBIL_L_003_DeliverySeparateFromApproval(t *testing.T) {
	at := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	when := at.Add(time.Hour)
	tests := []struct {
		name   string
		status Status
	}{
		{"draft", StatusDraft},
		{"pending", StatusPendingApproval},
		{"approved", StatusApproved},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := pendingBill()
			b.Status = tc.status
			if _, err := applyChange(b, Change{DeliveredAt: &when}); !errors.Is(err, ErrReadOnlyField) {
				t.Fatalf("update delivered_at err = %v, want ErrReadOnlyField", err)
			}
		})
	}
	b, err := applyApproval(pendingBill(), approvalAt(at), "INV-2026-0001", false)
	if err != nil {
		t.Fatal(err)
	}
	if !b.DeliveredAt.IsZero() || !b.DeliveredOn.IsZero() || b.DeliveryChannel != "" {
		t.Fatalf("approval set delivery fields: %v %v %q", b.DeliveredAt, b.DeliveredOn, b.DeliveryChannel)
	}
}
