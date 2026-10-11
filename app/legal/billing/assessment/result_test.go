package assessment

import (
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/billing/interest"
	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/flags/flagsfake"
	"github.com/dagi377/counsel/kernel/money"
)

func resultOutcome(t *testing.T, on bool) Outcome {
	t.Helper()
	f := flagsfake.New()
	if err := f.Define(flags.Def{Name: FlagSolicitorsActVerified, Owner: "lawyer/LSO"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(FlagSolicitorsActVerified, on); err != nil {
		t.Fatal(err)
	}
	return Outcome{Calc: interest.Calc{}, Flags: f, RepaymentDays: 30}
}

// rule: BIL-L-011
// rule: BIL-L-033 (verify)
func TestBIL_L_011_AssessmentResultRepayment(t *testing.T) {
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	lines := []credit.Line{{BillLineID: "l1", FeeCents: 70796, TaxCents: 9204}}
	tests := []struct {
		name         string
		r            Result
		wantRefund   money.Cents
		wantInterest money.Cents
		wantCredit   int64
		wantGate     string
		wantErr      error
	}{
		{
			name: "paid 500000, assessed 420000 at 4%: refund 80000 + 1587",
			r: Result{BillID: "b1", BilledCents: 500000, AssessedCents: 420000, CertifiedOn: day(8, 1),
				Payments: []Payment{{On: day(2, 1), Cents: 500000}}, InterestRatePPM: 40000, Lines: lines},
			wantRefund: 80000, wantInterest: 1587, wantCredit: 80000, wantGate: RefundGate,
		},
		{
			name: "officer fixes no interest",
			r: Result{BillID: "b2", BilledCents: 500000, AssessedCents: 420000, CertifiedOn: day(8, 1),
				Payments: []Payment{{On: day(2, 1), Cents: 500000}}, Lines: lines},
			wantRefund: 80000, wantCredit: 80000, wantGate: RefundGate,
		},
		{
			name: "overpayment only on the later payment",
			r: Result{BillID: "b3", BilledCents: 500000, AssessedCents: 420000, CertifiedOn: day(8, 1),
				Payments: []Payment{{On: day(2, 1), Cents: 400000}, {On: day(5, 1), Cents: 100000}}, InterestRatePPM: 40000, Lines: lines},
			// 80000 overpaid on 05-01: rhu(80000*40000*92/365e6) = 807
			wantRefund: 80000, wantInterest: 807, wantCredit: 80000, wantGate: RefundGate,
		},
		{
			name: "unpaid bill: credit note only, no refund",
			r: Result{BillID: "b4", BilledCents: 500000, AssessedCents: 420000, CertifiedOn: day(8, 1),
				InterestRatePPM: 40000, Lines: lines},
			wantCredit: 80000,
		},
		{
			name:    "assessed above billed",
			r:       Result{BillID: "b5", BilledCents: 500000, AssessedCents: 600000, CertifiedOn: day(8, 1)},
			wantErr: ErrBadResult,
		},
		{
			name:    "credit lines do not match reduction",
			r:       Result{BillID: "b6", BilledCents: 500000, AssessedCents: 450000, CertifiedOn: day(8, 1), Lines: lines},
			wantErr: ErrBadResult,
		},
	}
	o := resultOutcome(t, true)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := o.Apply(tt.r, "clerk1")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Apply err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.RefundCents != tt.wantRefund || got.InterestCents != tt.wantInterest || got.TotalCents != tt.wantRefund+tt.wantInterest {
				t.Fatalf("refund %d interest %d total %d, want %d %d", got.RefundCents, got.InterestCents, got.TotalCents, tt.wantRefund, tt.wantInterest)
			}
			if got.CreditNote.TotalCents() != tt.wantCredit || got.CreditNote.Status != credit.StatusDraft || got.CreditNote.PreparedBy != "clerk1" {
				t.Fatalf("credit note = %+v, want draft of %d prepared by clerk1", got.CreditNote, tt.wantCredit)
			}
			if got.Gate != tt.wantGate {
				t.Fatalf("gate = %q, want %q", got.Gate, tt.wantGate)
			}
			// rule: BIL-L-033 (verify)
			if tt.wantRefund > 0 && !got.DueOn.Equal(tt.r.CertifiedOn.AddDate(0, 0, 30)) {
				t.Fatalf("due on %s, want certificate + 30 days", got.DueOn.Format(time.DateOnly))
			}
		})
	}
}

func TestAssessmentResultFlagOff(t *testing.T) {
	_, err := resultOutcome(t, false).Apply(Result{BillID: "b", BilledCents: 1, AssessedCents: 1}, "clerk1")
	if !errors.Is(err, ErrResultUnverified) {
		t.Fatalf("Apply err = %v, want ErrResultUnverified", err)
	}
}
