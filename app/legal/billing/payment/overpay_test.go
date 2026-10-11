package payment

import (
	"errors"
	"testing"
	"time"
)

func tday(m time.Month, d int) time.Time {
	return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC)
}

// weekdaysBetween counts weekdays after from, up to and including to.
func weekdaysBetween(from, to time.Time) int {
	n := 0
	for d := from.AddDate(0, 0, 1); !d.After(to); d = d.AddDate(0, 0, 1) {
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday {
			n++
		}
	}
	return n
}

// rule: BIL-C-063
func TestBIL_C_063_OverpaymentToClientCredit(t *testing.T) {
	// BIL-X-008: $2,500 over two bills of $1,130 leaves $240.
	bills := []OpenBill{
		{ID: "0131", ClientID: "c1", Number: "0131", DueDate: tday(8, 30), Open: Buckets{Fee: 100000, Tax: 13000}},
		{ID: "0148", ClientID: "c1", Number: "0148", DueDate: tday(10, 2), Open: Buckets{Fee: 100000, Tax: 13000}},
	}
	rows, err := Allocator{BillOrder: BillOldestDueFirst}.Allocate(Request{ClientID: "c1", Source: SourcePayment, DepositTo: AccountOperating, AmountCents: 250000, Bills: bills})
	if err != nil {
		t.Fatal(err)
	}
	c, ok := Overpayment("p1", "c1", tday(10, 7), rows)
	if !ok || c.AmountCents != 24000 || c.State != CreditUnapplied {
		t.Fatalf("overpayment = %+v ok=%v, want 24000 unapplied", c, ok)
	}
	if _, ok := Overpayment("p2", "c1", tday(10, 7), []Row{{BillID: "0131", AmountCents: 5}}); ok {
		t.Fatal("fully applied payment made a credit")
	}
	held, err := c.Hold(GateCredit)
	if err != nil || held.State != CreditHeld || held.Account != AccountClientCredit {
		t.Fatalf("hold = %+v, %v", held, err)
	}

	delivered := OpenBill{ID: "0155", ClientID: "c1", DeliveredAt: tday(10, 1)}
	tests := []struct {
		name  string
		from  Credit
		step  func(Credit) (Credit, error)
		want  CreditState
		error error
	}{
		{"hold needs G-B3", c, func(c Credit) (Credit, error) { return c.Hold(GateRefund) }, CreditUnapplied, ErrWrongGate},
		{"hold twice", held, func(c Credit) (Credit, error) { return c.Hold(GateCredit) }, CreditHeld, ErrCreditState},
		{"apply before G-B3", c, func(c Credit) (Credit, error) { return c.ApplyTo(delivered, GateCredit) }, CreditUnapplied, ErrCreditState},
		{"apply to delivered bill", held, func(c Credit) (Credit, error) { return c.ApplyTo(delivered, GateCredit) }, CreditApplied, nil},
		{"apply to another client", held, func(c Credit) (Credit, error) {
			return c.ApplyTo(OpenBill{ID: "x", ClientID: "c2", DeliveredAt: tday(10, 1)}, GateCredit)
		}, CreditHeld, ErrOtherClient},
		{"apply to undelivered bill", held, func(c Credit) (Credit, error) { return c.ApplyTo(OpenBill{ID: "y", ClientID: "c1"}, GateCredit) }, CreditHeld, ErrNotDelivered},
		{"apply under wrong gate", held, func(c Credit) (Credit, error) { return c.ApplyTo(delivered, GateTransfer) }, CreditHeld, ErrWrongGate},
		{"refund under G-B6", held, func(c Credit) (Credit, error) { return c.Refund(GateRefund) }, CreditRefunded, nil},
		{"refund under G-B3", held, func(c Credit) (Credit, error) { return c.Refund(GateCredit) }, CreditHeld, ErrWrongGate},
		{"to trust under G6", held, func(c Credit) (Credit, error) { return c.ToTrust(GateTransfer) }, CreditTransferred, nil},
		{"to trust under G-B6", held, func(c Credit) (Credit, error) { return c.ToTrust(GateRefund) }, CreditHeld, ErrWrongGate},
		{"to trust before G-B3", c, func(c Credit) (Credit, error) { return c.ToTrust(GateTransfer) }, CreditUnapplied, ErrCreditState},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.step(tc.from)
			if !errors.Is(err, tc.error) || got.State != tc.want {
				t.Fatalf("state=%s err=%v, want %s and %v", got.State, err, tc.want, tc.error)
			}
		})
	}
}

// rule: BIL-C-063
func TestBIL_C_063_StaleCreditException(t *testing.T) {
	// BIL-X-025: $240 created 2026-10-07; deadline 2026-10-14; open on 2026-10-15 -> exception.
	held := Credit{PaymentID: "p1", ClientID: "c1", AmountCents: 24000, CreatedOn: tday(10, 7), State: CreditHeld}
	tests := []struct {
		name    string
		credit  Credit
		today   time.Time
		maxDays int
		want    int
	}{
		{"5th business day", held, tday(10, 14), 0, 0},
		{"6th business day", held, tday(10, 15), 0, 1},
		{"unapplied also ages", Credit{PaymentID: "p1", ClientID: "c1", CreatedOn: tday(10, 7), State: CreditUnapplied}, tday(10, 15), 0, 1},
		{"applied credit", Credit{CreatedOn: tday(10, 7), State: CreditApplied}, tday(10, 30), 0, 0},
		{"refunded credit", Credit{CreatedOn: tday(10, 7), State: CreditRefunded}, tday(10, 30), 0, 0},
		{"moved to trust", Credit{CreatedOn: tday(10, 7), State: CreditTransferred}, tday(10, 30), 0, 0},
		{"firm limit 10", held, tday(10, 15), 10, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AgeCredits([]Credit{tc.credit}, tc.today, tc.maxDays, weekdaysBetween)
			if len(got) != tc.want {
				t.Fatalf("exceptions = %+v, want %d", got, tc.want)
			}
			if tc.want == 1 && got[0].Title != ExceptionClientMoney {
				t.Fatalf("title = %q", got[0].Title)
			}
		})
	}
}

// rule: BIL-C-054
func TestRV1_002_AllocationCappedByReserved(t *testing.T) {
	tests := []struct {
		name      string
		amount    int64
		reserved  int64
		toBill    int64
		unapplied int64
	}{
		{"no reservation", 120000, 0, 113000, 7000},
		{"reserved fee transfer", 113000, 50000, 63000, 50000},
		{"fully reserved", 10000, 113000, 0, 10000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := OpenBill{ID: "b1", ClientID: "c1", Number: "1", DeliveredAt: tday(10, 1), Open: Buckets{Fee: 100000, Tax: 13000}, ReservedCents: tc.reserved}
			rows, err := Allocator{}.Allocate(Request{ClientID: "c1", Source: SourcePayment, DepositTo: AccountOperating, AmountCents: tc.amount, TargetBillID: "b1", Bills: []OpenBill{b}})
			if err != nil {
				t.Fatal(err)
			}
			var bill, un int64
			for _, r := range rows {
				if r.BillID == "" {
					un += r.AmountCents
				} else {
					bill += r.AmountCents
				}
			}
			c, ok := Overpayment("p", "c1", tday(10, 7), rows)
			if bill != tc.toBill || un != tc.unapplied || ok != (tc.unapplied > 0) || c.AmountCents != tc.unapplied {
				t.Fatalf("bill=%d unapplied=%d credit=%d, want %d and %d", bill, un, c.AmountCents, tc.toBill, tc.unapplied)
			}
		})
	}
}
