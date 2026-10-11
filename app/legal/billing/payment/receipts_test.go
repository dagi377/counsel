package payment_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/billing/payment/paymentfake"
	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
)

// rule: BIL-I-016
// rule: BIL-L-042
func TestBIL_I_016_BIL_L_042_ReceiptsJournal(t *testing.T) {
	on := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		capture payment.Capture
		err     error
		lines   []payment.ReceiptLine
		paidB1  int64
	}{
		{"retainer into operating refused",
			payment.Capture{ClientID: "c1", BillID: "b1", Kind: payment.KindRetainer, AmountCents: 50000, ReceivedOn: on, IdempotencyKey: "k1"},
			payment.ErrRetainerToOperating, nil, 0},
		{"bill payment recorded against its bill",
			payment.Capture{ClientID: "c1", BillID: "b1", Kind: payment.KindBillPayment, AmountCents: 56500, ReceivedOn: on, IdempotencyKey: "k2"},
			nil, []payment.ReceiptLine{{BillID: "b1", AmountCents: 56500, Account: payment.AccountAR}}, 56500},
		{"overpayment shows client credit",
			payment.Capture{ClientID: "c1", BillID: "b1", Kind: payment.KindBillPayment, AmountCents: 137000, ReceivedOn: on, IdempotencyKey: "k3"},
			nil, []payment.ReceiptLine{
				{BillID: "b1", AmountCents: 113000, Account: payment.AccountAR},
				{AmountCents: 24000, Account: payment.AccountClientCredit},
			}, 113000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := legalflagstest.Context(t, payment.FlagReceiptsJournalVerified, true)
			svc := paymentfake.New()
			svc.AddBill("b1", "c1", 100000, 13000)
			var j payment.ReceiptsJournal
			// The journal refuses a retainer even if capture let it through.
			p := payment.Payment{ID: "pay_r", Capture: tc.capture, Status: payment.StatusCaptured}
			var as []payment.Allocation
			if tc.capture.Kind != payment.KindRetainer {
				var err error
				if p, as, err = svc.Capture(ctx, tc.capture); err != nil {
					t.Fatal(err)
				}
			}
			r, err := j.Record(ctx, p, as)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err=%v, want %v", err, tc.err)
			}
			if tc.err != nil {
				if len(j.Entries()) != 0 {
					t.Fatal("refused receipt was journalled")
				}
				return
			}
			// Recording the same payment again keeps one entry.
			if _, err := j.Record(ctx, p, as); err != nil {
				t.Fatal(err)
			}
			if n := len(j.Entries()); n != 1 {
				t.Fatalf("entries=%d, want 1", n)
			}
			if len(r.Lines) != len(tc.lines) {
				t.Fatalf("lines=%+v, want %+v", r.Lines, tc.lines)
			}
			for i, l := range tc.lines {
				if r.Lines[i] != l {
					t.Fatalf("line %d = %+v, want %+v", i, r.Lines[i], l)
				}
			}
			if got := j.PaidTo("b1"); got != tc.paidB1 {
				t.Fatalf("paid to b1 = %d, want %d", got, tc.paidB1)
			}
		})
	}
}

// receiptsFlag turns FlagReceiptsJournalVerified on or off.
type receiptsFlag bool

func (f receiptsFlag) Enabled(name string) bool {
	return bool(f) && name == payment.FlagReceiptsJournalVerified
}

// rule: BIL-L-042 (verify)
// While the flag is off, unapplied money is refused (fail closed); bill payments still record.
func TestBIL_L_042_ClientCreditFailsClosedWhenUnverified(t *testing.T) {
	ctx := context.Background()
	on := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		flags  interface{ Enabled(string) bool }
		amount int64
		err    error
	}{
		{"overpayment, flag off", receiptsFlag(false), 137000, payment.ErrUnverified},
		{"overpayment, no flags", nil, 137000, payment.ErrUnverified},
		{"exact payment, flag off", receiptsFlag(false), 113000, nil},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := paymentfake.New()
			svc.AddBill("b1", "c1", 100000, 13000)
			p, as, err := svc.Capture(ctx, payment.Capture{ClientID: "c1", BillID: "b1", Kind: payment.KindBillPayment, AmountCents: tc.amount, ReceivedOn: on, IdempotencyKey: "k" + string(rune('a'+i))})
			if err != nil {
				t.Fatal(err)
			}
			jctx := context.Background()
			if tc.flags != nil {
				jctx = legalflagstest.Context(t, payment.FlagReceiptsJournalVerified, tc.flags.Enabled(payment.FlagReceiptsJournalVerified))
			}
			var j payment.ReceiptsJournal
			if _, err := j.Record(jctx, p, as); !errors.Is(err, tc.err) {
				t.Fatalf("err=%v, want %v", err, tc.err)
			}
			if tc.err != nil && len(j.Entries()) != 0 {
				t.Fatal("refused receipt was journalled")
			}
		})
	}
}

func TestReceiptsJournalRefusesBadInput(t *testing.T) {
	ok := payment.Capture{ClientID: "c1", BillID: "b1", Kind: payment.KindBillPayment, AmountCents: 1000}
	tests := []struct {
		name   string
		p      payment.Payment
		allocs []payment.Allocation
		err    error
	}{
		{"no id", payment.Payment{Capture: ok}, nil, payment.ErrInvalid},
		{"reversed", payment.Payment{ID: "p", Capture: ok, Status: payment.StatusReversed}, nil, payment.ErrReversed},
		{"lines short of amount", payment.Payment{ID: "p", Capture: ok}, []payment.Allocation{{PaymentID: "p", BillID: "b1", FeeCents: 900}}, payment.ErrUnbalanced},
		{"other payment's allocation", payment.Payment{ID: "p", Capture: ok}, []payment.Allocation{{PaymentID: "q", BillID: "b1", FeeCents: 1000}}, payment.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var j payment.ReceiptsJournal
			if _, err := j.Record(context.Background(), tc.p, tc.allocs); !errors.Is(err, tc.err) {
				t.Fatalf("err=%v, want %v", err, tc.err)
			}
		})
	}
}
