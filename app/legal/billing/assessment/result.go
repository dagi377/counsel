package assessment

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/billing/interest"
	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/money"
)

// Assessment result errors.
var (
	// ErrResultUnverified means the Solicitors Act flag is off.
	ErrResultUnverified = errors.New("assessment: results disabled until Solicitors Act rules are verified")
	// ErrBadResult means the assessed amount is negative or above the billed amount, or the
	// credit lines do not add up to the reduction.
	ErrBadResult = errors.New("assessment: bad result")
)

// RefundGate is the approval gate every refund of an overpayment goes through.
const RefundGate = "G-B6"

// Payment is one payment the client made on the assessed bill. On is a business date.
type Payment struct {
	On    time.Time
	Cents money.Cents
}

// Result is the assessment officer's certificate for one bill.
type Result struct {
	BillID        string
	BilledCents   money.Cents // bill total, fee plus tax
	AssessedCents money.Cents // amount the officer allowed
	CertifiedOn   time.Time   // certificate date (business date)
	Payments      []Payment
	// InterestRatePPM is the rate the officer fixed on the overpayment; 0 means none.
	InterestRatePPM money.PPM
	// Lines splits the reduction (BilledCents - AssessedCents) over the bill lines, fee
	// plus prorated tax. They become the credit note.
	Lines []credit.Line
}

// Repayment is what the firm owes the client after an assessment that reduced fees.
type Repayment struct {
	BillID        string
	CreditNote    credit.Note // draft; approved through the credit module
	RefundCents   money.Cents // paid - assessed, never below 0
	InterestCents money.Cents // on each overpaid payment, from its date to the certificate
	TotalCents    money.Cents // refund plus interest
	DueOn         time.Time   // days_after(certificate date, RepaymentDays)
	Gate          string      // RefundGate when a refund is owed, else ""
}

// Outcome turns an assessment result into a credit note and a refund task.
type Outcome struct {
	Calc  interest.Calculator
	Flags flags.Flags
	// RepaymentDays is the firm's repayment window in days after the certificate.
	RepaymentDays int
}

// Apply computes the credit note, the refund of any overpayment and the interest on it.
// preparedBy prepares the credit note; someone else approves it.
func (o Outcome) Apply(r Result, preparedBy string) (Repayment, error) {
	if !o.Flags.Enabled(FlagSolicitorsActVerified) {
		return Repayment{}, ErrResultUnverified
	}
	if r.AssessedCents < 0 || r.AssessedCents > r.BilledCents || o.RepaymentDays < 0 {
		return Repayment{}, fmt.Errorf("bill %s: %w", r.BillID, ErrBadResult)
	}
	out := Repayment{BillID: r.BillID}
	if reduction := r.BilledCents - r.AssessedCents; reduction > 0 {
		var sum money.Cents
		for _, l := range r.Lines {
			sum += money.Cents(l.FeeCents + l.TaxCents)
		}
		if sum != reduction {
			return Repayment{}, fmt.Errorf("bill %s: credit lines %d, reduction %d: %w", r.BillID, sum, reduction, ErrBadResult)
		}
		// rule: BIL-L-011
		out.CreditNote = credit.Note{
			BillID:     r.BillID,
			Lines:      append([]credit.Line(nil), r.Lines...),
			Reason:     "assessment certificate " + r.CertifiedOn.Format(time.DateOnly),
			Status:     credit.StatusDraft,
			PreparedBy: preparedBy,
		}
	}
	var paid money.Cents
	for _, p := range r.Payments {
		paid += p.Cents
	}
	over := paid - r.AssessedCents
	if over <= 0 {
		return out, nil
	}
	// rule: BIL-L-011
	interestCents, err := o.overpaymentInterest(r, over)
	if err != nil {
		return Repayment{}, fmt.Errorf("bill %s: %w", r.BillID, err)
	}
	// rule: BIL-L-033 (verify)
	out.RefundCents = over
	out.InterestCents = interestCents
	out.TotalCents = over + interestCents
	out.DueOn = r.CertifiedOn.AddDate(0, 0, o.RepaymentDays)
	out.Gate = RefundGate
	return out, nil
}

// overpaymentInterest charges the officer's rate on the overpaid part of each payment, from
// its payment date to the certificate date. The latest payments are the overpaid ones.
func (o Outcome) overpaymentInterest(r Result, over money.Cents) (money.Cents, error) {
	if r.InterestRatePPM <= 0 {
		return 0, nil
	}
	pays := append([]Payment(nil), r.Payments...)
	sort.SliceStable(pays, func(i, j int) bool { return pays[i].On.After(pays[j].On) })
	var total money.Cents
	for _, p := range pays {
		if over <= 0 {
			break
		}
		part := min(p.Cents, over)
		over -= part
		days := daysBetween(p.On, r.CertifiedOn)
		if days <= 0 {
			continue
		}
		v, err := o.Calc.Simple(part, r.InterestRatePPM, days)
		if err != nil {
			return 0, fmt.Errorf("overpayment interest: %w", err)
		}
		total += v
	}
	return total, nil
}

// daysBetween counts calendar days from a to b, ignoring time of day and DST.
func daysBetween(a, b time.Time) int {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return int(time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC).Sub(time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC)).Hours() / 24)
}
