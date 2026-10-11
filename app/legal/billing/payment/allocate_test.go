package payment

import (
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

func day(d int) time.Time { return time.Date(2026, 3, d, 0, 0, 0, 0, time.UTC) }

func alloc(pre PreTaxOrder, order BillOrder) Allocator {
	return Allocator{Money: moneyfake.New(), PreTax: pre, BillOrder: order}
}

func bill(id string, due int, open Buckets) OpenBill {
	return OpenBill{ID: id, ClientID: "c1", Number: id, DueDate: day(due), DeliveredAt: day(1), Open: open}
}

// rule: BIL-C-202
func TestBIL_C_202_TaxShare(t *testing.T) {
	tests := []struct {
		name string
		p    int64
		open Buckets
		want Buckets
	}{
		{"spec example", 50000, Buckets{Fee: 80000, Tax: 10400}, Buckets{Fee: 44248, Tax: 5752}},
		{"clearing principal takes exact tax", 90400, Buckets{Fee: 80000, Tax: 10400}, Buckets{Fee: 80000, Tax: 10400}},
		{"one cent", 1, Buckets{Fee: 80000, Tax: 10400}, Buckets{Fee: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := alloc(PreTaxFeesFirst, BillOldestDueFirst).Split(tc.p, tc.open)
			if err != nil || got != tc.want {
				t.Fatalf("Split = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

// rule: BIL-C-061
// rule: BIL-C-060
func TestBIL_C_061_PreTaxBuckets(t *testing.T) {
	open := Buckets{Fee: 1000, Dtx: 500, Dnt: 300, Interest: 200}
	tests := []struct {
		name string
		pre  PreTaxOrder
		p    int64
		want Buckets
	}{
		{"fees first", PreTaxFeesFirst, 1200, Buckets{Fee: 1000, Dtx: 200}},
		{"spills to dnt", PreTaxFeesFirst, 1700, Buckets{Fee: 1000, Dtx: 500, Dnt: 200}},
		{"principal before interest", PreTaxFeesFirst, 1900, Buckets{Fee: 1000, Dtx: 500, Dnt: 300, Interest: 100}},
		{"proportional largest remainder", PreTaxProportional, 900, Buckets{Fee: 500, Dtx: 250, Dnt: 150}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := alloc(tc.pre, BillOldestDueFirst).Split(tc.p, open)
			if err != nil || got != tc.want {
				t.Fatalf("Split = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

// rule: BIL-C-060
func TestBIL_C_060_PrincipalBeforeInterest(t *testing.T) {
	got, err := alloc(PreTaxFeesFirst, BillOldestDueFirst).Split(100, Buckets{Fee: 100, Interest: 50})
	if err != nil || got != (Buckets{Fee: 100}) {
		t.Fatalf("Split = %+v, %v", got, err)
	}
}

// rule: BIL-T-082
func TestBIL_T_082_TaxIndependentOfOrder(t *testing.T) {
	open := Buckets{Fee: 70000, Dtx: 10000, Dnt: 5000, Tax: 10400}
	for _, p := range []int64{1, 333, 50000, 95399} {
		a, _ := alloc(PreTaxFeesFirst, BillOldestDueFirst).Split(p, open)
		b, _ := alloc(PreTaxProportional, BillOldestDueFirst).Split(p, open)
		if a.Tax != b.Tax {
			t.Fatalf("p=%d tax fees_first %d != proportional %d", p, a.Tax, b.Tax)
		}
	}
}

// rule: BIL-C-062
func TestBIL_C_062_BillOrder(t *testing.T) {
	b1 := bill("B-2", 10, Buckets{Fee: 100})
	b2 := bill("B-1", 5, Buckets{Fee: 100})
	b3 := bill("B-3", 5, Buckets{Fee: 100})
	tests := []struct {
		name   string
		order  BillOrder
		target string
		amount int64
		want   []Row
	}{
		{"oldest due first, number breaks tie", BillOldestDueFirst, "", 250,
			[]Row{{"B-1", 100, Buckets{Fee: 100}}, {"B-3", 100, Buckets{Fee: 100}}, {"B-2", 50, Buckets{Fee: 50}}}},
		{"named bill, excess unapplied", BillOldestDueFirst, "B-2", 150,
			[]Row{{"B-2", 100, Buckets{Fee: 100}}, {"", 50, Buckets{}}}},
		{"manual leaves unapplied", BillManual, "", 80, []Row{{"", 80, Buckets{}}}},
		{"proportional", BillProportional, "", 150,
			[]Row{{"B-1", 50, Buckets{Fee: 50}}, {"B-3", 50, Buckets{Fee: 50}}, {"B-2", 50, Buckets{Fee: 50}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := alloc(PreTaxFeesFirst, tc.order).Allocate(Request{
				ClientID: "c1", Source: SourcePayment, DepositTo: AccountOperating,
				AmountCents: tc.amount, TargetBillID: tc.target, Bills: []OpenBill{b1, b2, b3},
			})
			if err != nil || len(rows) != len(tc.want) {
				t.Fatalf("rows = %+v, %v; want %+v", rows, err, tc.want)
			}
			for i := range rows {
				if rows[i] != tc.want[i] {
					t.Fatalf("row %d = %+v, want %+v", i, rows[i], tc.want[i])
				}
			}
		})
	}
}

// rule: BIL-I-005
// rule: ARC-I-029
func TestBIL_I_005_OperatingOnly(t *testing.T) {
	tests := []struct {
		name string
		to   Account
		err  error
	}{
		{"operating allocates", AccountOperating, nil},
		{"trust receipt rejected", AccountTrust, ErrTrustReceipt},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := alloc(PreTaxFeesFirst, BillOldestDueFirst).Allocate(Request{
				ClientID: "c1", Source: SourcePayment, DepositTo: tc.to, AmountCents: 100,
				Bills: []OpenBill{bill("B-1", 1, Buckets{Fee: 100})},
			})
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

// rule: ARC-I-029
func TestARC_I_029_RowsSumToAmount(t *testing.T) {
	tests := []struct {
		name string
		rows []Row
		err  error
	}{
		{"balanced", []Row{{"B-1", 60, Buckets{Fee: 60}}, {"", 40, Buckets{}}}, nil},
		{"short", []Row{{"B-1", 60, Buckets{Fee: 60}}}, ErrUnbalanced},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckRows(100, tc.rows, nil); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

// rule: BIL-C-054
// rule: ARC-I-029
func TestBIL_C_054_ReservedCapsAllocation(t *testing.T) {
	b := bill("B-1", 1, Buckets{Fee: 1000})
	b.ReservedCents = 700
	rows, err := alloc(PreTaxFeesFirst, BillOldestDueFirst).Allocate(Request{
		ClientID: "c1", Source: SourcePayment, DepositTo: AccountOperating, AmountCents: 500, Bills: []OpenBill{b},
	})
	if err != nil || len(rows) != 2 || rows[0].AmountCents != 300 || rows[1].BillID != "" || rows[1].AmountCents != 200 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
}

// rule: BIL-I-017
// rule: BIL-C-066
func TestBIL_I_017_SameClientOnly(t *testing.T) {
	other := bill("B-9", 1, Buckets{Fee: 100})
	other.ClientID = "c2"
	undelivered := bill("B-2", 1, Buckets{Fee: 100})
	undelivered.DeliveredAt = time.Time{}
	tests := []struct {
		name   string
		bills  []OpenBill
		target string
		err    error
	}{
		{"other client's bill", []OpenBill{other}, "", ErrOtherClient},
		{"credit to undelivered bill", []OpenBill{undelivered}, "B-2", ErrNotDelivered},
		{"credit to delivered bill", []OpenBill{bill("B-1", 1, Buckets{Fee: 100})}, "B-1", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := alloc(PreTaxFeesFirst, BillOldestDueFirst).Allocate(Request{
				ClientID: "c1", Source: SourceClientCredit, AmountCents: 50, TargetBillID: tc.target, Bills: tc.bills,
			})
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

// rule: BIL-C-066
func TestBIL_C_066_CreditAllocatedLikePayment(t *testing.T) {
	rows, err := alloc(PreTaxFeesFirst, BillOldestDueFirst).Allocate(Request{
		ClientID: "c1", Source: SourceClientCredit, AmountCents: 50000,
		Bills: []OpenBill{bill("B-1", 1, Buckets{Fee: 80000, Tax: 10400})},
	})
	if err != nil || len(rows) != 1 || rows[0].Buckets != (Buckets{Fee: 44248, Tax: 5752}) {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
}

// rule: BIL-I-026
func TestBIL_I_026_RowBuckets(t *testing.T) {
	bills := map[string]OpenBill{"B-1": bill("B-1", 1, Buckets{Fee: 50, Tax: 10})}
	tests := []struct {
		name string
		rows []Row
		err  error
	}{
		{"ok", []Row{{"B-1", 60, Buckets{Fee: 50, Tax: 10}}}, nil},
		{"buckets do not sum", []Row{{"B-1", 60, Buckets{Fee: 50}}, {"", 10, Buckets{}}}, ErrUnbalanced},
		{"unapplied with buckets", []Row{{"B-1", 50, Buckets{Fee: 50}}, {"", 10, Buckets{Tax: 10}}}, ErrUnbalanced},
		{"bucket over cap", []Row{{"B-1", 60, Buckets{Fee: 60}}}, ErrUnbalanced},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckRows(60, tc.rows, bills); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

// rule: BIL-I-012
func TestBIL_I_012_PaymentBalances(t *testing.T) {
	rng := rand.New(rand.NewSource(12))
	for _, order := range []BillOrder{BillOldestDueFirst, BillProportional, BillManual} {
		for _, pre := range []PreTaxOrder{PreTaxFeesFirst, PreTaxProportional} {
			a := alloc(pre, order)
			for i := 0; i < 300; i++ {
				var bills []OpenBill
				byID := map[string]OpenBill{}
				for j := 0; j < 1+rng.Intn(4); j++ {
					b := bill(string(rune('A'+j)), 1+rng.Intn(20), Buckets{
						Fee: rng.Int63n(100000), Dtx: rng.Int63n(5000), Dnt: rng.Int63n(5000),
						Tax: rng.Int63n(13000), Interest: rng.Int63n(2000),
					})
					b.ReservedCents = rng.Int63n(20000)
					bills = append(bills, b)
					byID[b.ID] = b
				}
				amount := 1 + rng.Int63n(300000)
				rows, err := a.Allocate(Request{ClientID: "c1", Source: SourcePayment,
					DepositTo: AccountOperating, AmountCents: amount, Bills: bills})
				if err != nil {
					t.Fatalf("Allocate: %v", err)
				}
				if err := CheckRows(amount, rows, byID); err != nil {
					t.Fatalf("CheckRows: %v (%+v)", err, rows)
				}
				refund := rng.Int63n(amount + 1)
				if err := CheckBalance(amount+refund, rows, refund, 0); err != nil {
					t.Fatalf("CheckBalance: %v", err)
				}
				if err := CheckBalance(amount, rows, 1, 0); !errors.Is(err, ErrUnbalanced) {
					t.Fatalf("CheckBalance off by one = %v", err)
				}
			}
		}
	}
}

// rule: BIL-C-064
func TestBIL_C_064_Refund(t *testing.T) {
	base := Refund{ClientID: "c1", AmountCents: 100, CreditCents: 100, OriginalCaptured: 100,
		OriginalMethodRef: "card", RequestedBy: "u1", ApprovedBy: "u2", ApproverIsLawyer: true}
	tests := []struct {
		name  string
		mod   func(*Refund)
		route RefundRoute
		err   error
	}{
		{"original method", func(*Refund) {}, RefundOriginalMethod, nil},
		{"above captured goes to operating", func(r *Refund) { r.OriginalCaptured = 50 }, RefundOperating, nil},
		{"exceeds credit", func(r *Refund) { r.AmountCents = 101 }, "", ErrRefundExceedsCredit},
		{"requester approves", func(r *Refund) { r.ApprovedBy = "u1" }, "", ErrPreparerApproves},
		{"non-lawyer approves", func(r *Refund) { r.ApproverIsLawyer = false }, "", ErrPreparerApproves},
		{"agent approves", func(r *Refund) { r.ApproverIsAgent = true }, "", ErrPreparerApproves},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mod(&r)
			route, err := CheckRefund(r)
			if !errors.Is(err, tc.err) || route != tc.route {
				t.Fatalf("CheckRefund = %s, %v; want %s, %v", route, err, tc.route, tc.err)
			}
		})
	}
}

// rule: BIL-C-067
func TestBIL_C_067_CreditAging(t *testing.T) {
	weekdays := func(from, to time.Time) int {
		n := 0
		for d := from.AddDate(0, 0, 1); !d.After(to); d = d.AddDate(0, 0, 1) {
			if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
				n++
			}
		}
		return n
	}
	tests := []struct {
		name    string
		today   time.Time
		maxDays int
		want    bool
	}{
		{"within default 5", day(9), 0, false}, // Mon 2 -> Mon 9 is 5 business days
		{"past default 5", day(10), 0, true},   // 6 business days
		{"firm limit 10", day(10), 10, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CreditOverdue(day(2), tc.today, tc.maxDays, weekdays); got != tc.want {
				t.Fatalf("CreditOverdue = %v, want %v", got, tc.want)
			}
		})
	}
}
