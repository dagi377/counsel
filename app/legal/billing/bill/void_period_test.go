package bill

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestBIL_C_123_VoidAcrossPeriods(t *testing.T) {
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	tests := []struct {
		name     string
		billDate time.Time
		voidDate time.Time
		filed    bool
		want     VoidTax
		err      error
	}{
		// rule: BIL-C-123
		// BIL-X-031: approved 2026-09-30, Q3 filed 2026-10-03, void 2026-10-05 -> Q4 adjustment.
		{name: "bill period filed: negative adjustment in void period", billDate: day(9, 30), voidDate: day(10, 5), filed: true,
			want: VoidTax{Kind: VoidTaxAdjustment, On: day(10, 5)}},
		{name: "same unfiled period nets to zero", billDate: day(9, 10), voidDate: day(9, 25), filed: false,
			want: VoidTax{Kind: VoidTaxNets, On: day(9, 25)}},
		{name: "void on the bill date", billDate: day(9, 30), voidDate: day(9, 30), filed: false,
			want: VoidTax{Kind: VoidTaxNets, On: day(9, 30)}},
		{name: "void before bill date", billDate: day(9, 30), voidDate: day(9, 29), err: ErrInvalid},
		{name: "zero void date", billDate: day(9, 30), err: ErrInvalid},
		{name: "zero bill date", voidDate: day(9, 30), err: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := VoidTaxEffect(tt.billDate, tt.voidDate, tt.filed)
			if !errors.Is(err, tt.err) {
				t.Fatalf("VoidTaxEffect err = %v, want %v", err, tt.err)
			}
			if got != tt.want {
				t.Fatalf("VoidTaxEffect = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBIL_C_123_PeriodCrossWarnings(t *testing.T) {
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	date := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, toronto) }
	periodEnd := date(9, 30)
	approved := func(id ID, issued time.Time) Bill {
		return Bill{ID: id, Number: "INV-2026-" + string(id), Status: StatusApproved, IssueDate: issued}
	}
	deliveredAt := func(b Bill, at time.Time) Bill {
		b.Status = StatusDelivered
		b.DeliveredAt = at
		return b
	}
	tests := []struct {
		name  string
		bills []Bill
		want  []PeriodWarning
	}{
		// rule: BIL-C-123
		{name: "approved undelivered at period end warns", bills: []Bill{approved("0147", date(9, 30))},
			want: []PeriodWarning{{Code: WarnPeriodCross, Bill: "0147", Number: "INV-2026-0147"}}},
		{name: "approved after period end does not warn", bills: []Bill{approved("0148", date(10, 1))}},
		{name: "delivered within period does not warn",
			bills: []Bill{deliveredAt(approved("0149", date(9, 29)), time.Date(2026, 9, 30, 23, 30, 0, 0, toronto))}},
		{name: "delivered after period end still warns",
			bills: []Bill{deliveredAt(approved("0150", date(9, 29)), time.Date(2026, 10, 1, 0, 30, 0, 0, toronto))},
			want:  []PeriodWarning{{Code: WarnPeriodCross, Bill: "0150", Number: "INV-2026-0150"}}},
		{name: "delivered late evening Toronto is the same business day",
			bills: []Bill{deliveredAt(approved("0151", date(9, 29)), time.Date(2026, 10, 1, 3, 30, 0, 0, time.UTC))}},
		{name: "drafts, pending and void bills do not warn", bills: []Bill{
			{ID: "d", Status: StatusDraft},
			{ID: "p", Status: StatusPendingApproval},
			{ID: "v", Status: StatusVoid, Number: "INV-2026-0152", IssueDate: date(9, 1)},
		}},
		{name: "several bills keep input order", bills: []Bill{
			approved("0153", date(9, 2)),
			deliveredAt(approved("0154", date(9, 3)), date(9, 4).Add(10*time.Hour)),
			approved("0155", date(9, 5)),
		}, want: []PeriodWarning{
			{Code: WarnPeriodCross, Bill: "0153", Number: "INV-2026-0153"},
			{Code: WarnPeriodCross, Bill: "0155", Number: "INV-2026-0155"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PeriodCrossWarnings(tt.bills, periodEnd)
			if err != nil {
				t.Fatalf("PeriodCrossWarnings: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("PeriodCrossWarnings = %+v, want %+v", got, tt.want)
			}
		})
	}
}
