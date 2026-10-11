package bill

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func voidTestBill(status Status, delivered bool) Bill {
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		panic(err)
	}
	b := Bill{
		ID:         "b-150",
		Status:     status,
		PreparedBy: "u-prep",
		Lines: []Line{
			{Ordinal: 1, Source: SourceTime, SourceID: "te-1", AmountCents: 60000},
			{Ordinal: 2, Source: SourceTime, SourceID: "te-2", AmountCents: 40000},
			{Ordinal: 3, Source: SourceExpense, SourceID: "ex-1", AmountCents: 2500},
			{Ordinal: 4, Source: SourceAdjustment, AmountCents: -2500},
		},
		FeesCents:  100000,
		TaxCents:   13000,
		TotalCents: 113000,
	}
	if status != StatusDraft && status != StatusPendingApproval && status != StatusPreflightFailed {
		b.Number = "INV-2026-0150"
		b.IssueDate = time.Date(2026, 10, 6, 0, 0, 0, 0, toronto)
		b.ApprovalID = "g-1"
		b.ApprovedBy = "u-lawyer"
	}
	if delivered {
		b.DeliveredAt = time.Date(2026, 10, 7, 14, 0, 0, 0, toronto)
		b.DeliveredOn = time.Date(2026, 10, 7, 0, 0, 0, 0, toronto)
	}
	return b
}

func TestBIL_C_084_VoidKeepsNumber(t *testing.T) {
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	on := time.Date(2026, 10, 8, 0, 0, 0, 0, toronto)
	lawyer := Voider{UserID: "u-lawyer"}
	tests := []struct {
		name string
		b    Bill
		by   Voider
		on   time.Time
		want VoidPlan
		err  error
	}{
		// rule: BIL-C-084
		{
			name: "draft is discarded without a number",
			b:    voidTestBill(StatusDraft, false), by: Voider{UserID: "u-prep"}, on: on,
			want: VoidPlan{Action: VoidDiscard, Bill: "b-150", Status: StatusVoid},
		},
		{
			name: "pending approval is discarded without a number",
			b:    voidTestBill(StatusPendingApproval, false), by: lawyer, on: on,
			want: VoidPlan{Action: VoidDiscard, Bill: "b-150", Status: StatusVoid},
		},
		{
			name: "preflight failed is discarded",
			b:    voidTestBill(StatusPreflightFailed, false), by: lawyer, on: on,
			want: VoidPlan{Action: VoidDiscard, Bill: "b-150", Status: StatusVoid},
		},
		{
			name: "approved undelivered keeps number, reverses on void date, frees entries",
			b:    voidTestBill(StatusApproved, false), by: lawyer, on: on,
			want: VoidPlan{
				Action:       VoidReverse,
				Bill:         "b-150",
				Number:       "INV-2026-0150",
				ReverseOn:    on,
				TimeEntryIDs: []string{"te-1", "te-2"},
				ExpenseIDs:   []string{"ex-1"},
				Status:       StatusVoid,
			},
		},
		{
			name: "void on the approval date is allowed",
			b:    voidTestBill(StatusApproved, false), by: lawyer, on: time.Date(2026, 10, 6, 0, 0, 0, 0, toronto),
			want: VoidPlan{
				Action:       VoidReverse,
				Bill:         "b-150",
				Number:       "INV-2026-0150",
				ReverseOn:    time.Date(2026, 10, 6, 0, 0, 0, 0, toronto),
				TimeEntryIDs: []string{"te-1", "te-2"},
				ExpenseIDs:   []string{"ex-1"},
				Status:       StatusVoid,
			},
		},
		{name: "preparer cannot void", b: voidTestBill(StatusApproved, false), by: Voider{UserID: "u-prep"}, on: on, err: ErrPreparerApproves},
		{name: "agent cannot void", b: voidTestBill(StatusApproved, false), by: Voider{UserID: "u-lawyer", IsAgent: true}, on: on, err: ErrPreparerApproves},
		{name: "anonymous cannot void", b: voidTestBill(StatusApproved, false), by: Voider{}, on: on, err: ErrPreparerApproves},
		{name: "void date before bill date", b: voidTestBill(StatusApproved, false), by: lawyer, on: time.Date(2026, 10, 5, 0, 0, 0, 0, toronto), err: ErrInvalid},
		{name: "zero void date", b: voidTestBill(StatusApproved, false), by: lawyer, err: ErrInvalid},
		{name: "already void", b: voidTestBill(StatusVoid, false), by: lawyer, on: on, err: ErrBillGuard},
		{name: "credited", b: voidTestBill(StatusCredited, true), by: lawyer, on: on, err: ErrBillGuard},
		{name: "written off", b: voidTestBill(StatusWrittenOff, true), by: lawyer, on: on, err: ErrBillGuard},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := planVoid(tt.b, tt.by, tt.on)
			if !errors.Is(err, tt.err) {
				t.Fatalf("planVoid err = %v, want %v", err, tt.err)
			}
			if tt.err != nil {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("planVoid = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBIL_C_084_DeliveredUseCreditNote(t *testing.T) {
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	on := time.Date(2026, 10, 8, 0, 0, 0, 0, toronto)
	tests := []struct {
		name string
		b    Bill
		by   Voider
	}{
		// rule: ARC-I-050
		{name: "approved with delivery evidence", b: voidTestBill(StatusApproved, true), by: Voider{UserID: "u-lawyer"}},
		{name: "delivered", b: voidTestBill(StatusDelivered, true), by: Voider{UserID: "u-lawyer"}},
		{name: "partially paid", b: voidTestBill(StatusPartiallyPaid, true), by: Voider{UserID: "u-lawyer"}},
		{name: "paid", b: voidTestBill(StatusPaid, true), by: Voider{UserID: "u-lawyer"}},
		{name: "delivered beats the preparer check", b: voidTestBill(StatusDelivered, true), by: Voider{UserID: "u-prep"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := planVoid(tt.b, tt.by, on)
			if !errors.Is(err, ErrUseCreditNote) {
				t.Fatalf("planVoid err = %v, want ErrUseCreditNote", err)
			}
			if !reflect.DeepEqual(got, VoidPlan{}) {
				t.Fatalf("planVoid plan = %+v, want none", got)
			}
		})
	}
	if ProblemDeliveredUseCreditNote != "bill_delivered_use_credit_note" {
		t.Fatalf("problem code = %q", ProblemDeliveredUseCreditNote)
	}
}
