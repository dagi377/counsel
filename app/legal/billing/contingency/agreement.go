package contingency

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/money"
)

// agreementFlag is legal.FlagContingencyFees (V-CFA). It is spelled out here because app/legal
// imports this package. Every Agreements method refuses with ErrAgreementsOff while it is off.
const agreementFlag = "billing.contingency_fees"

// ErrAgreementsOff is returned by every Agreements method while billing.contingency_fees is off.
var ErrAgreementsOff = errors.New("contingency: contingency fees are not enabled")

// StandardForm is the LSO Standard Form Contingency Fee Agreement a percentage CFA must use
// unless an exemption is recorded (BIL-L-017). (verify) the current LSO version.
const StandardForm = "LSO-SFCFA-2021-11-18"

// exemptAssetsOrRevenueCents is the organization threshold of BIL-L-017; the value must exceed it.
const exemptAssetsOrRevenueCents int64 = 1_000_000_000

// exemptEmployees is the organization head count of BIL-L-017; the count must exceed it.
const exemptEmployees = 25

// PracticeArea is the practice area of the matter a CFA belongs to.
type PracticeArea string

// Practice areas in which a CFA is not permitted (BIL-L-016). Others are allowed.
const (
	AreaCriminal      PracticeArea = "criminal"
	AreaQuasiCriminal PracticeArea = "quasi_criminal"
	AreaFamily        PracticeArea = "family"
)

// Kind says how the CFA computes the fee.
type Kind string

// CFA kinds.
const (
	// KindPercentage takes a percentage of the recovery.
	KindPercentage Kind = "percentage"
	// KindNonPercentage is any other contingent method, such as a success fee.
	KindNonPercentage Kind = "non_percentage"
)

// Clause tags that a CFA may not carry (BIL-L-019).
const (
	TagConsentToSettle     = "consent_to_settle"
	TagNoTermination       = "no_termination"
	TagFeeSplitNonLicensee = "fee_split_non_licensee"
)

// Statement numbers of the eight prescribed statements of a non-percentage CFA (BIL-L-018).
// StatementDisability is required only when the client is under disability.
const (
	StatementCount      = 8
	StatementDisability = 6
)

// Exemption is the recorded evidence that lets a percentage CFA use a form other than the
// standard form (BIL-L-017). EvidenceDocID is required for an organization exemption.
type Exemption struct {
	OrgEmployees            int
	OrgHasFullTimeLawyer    bool
	OrgAssetsOrRevenueCents int64
	EvidenceDocID           string
	CourtApprovalDocID      string
}

// exempt reports whether e exempts a CFA from the standard form and prescribed statements.
func (e *Exemption) exempt() bool {
	if e == nil {
		return false
	}
	if e.CourtApprovalDocID != "" {
		return true
	}
	if e.EvidenceDocID == "" {
		return false
	}
	return e.OrgEmployees > exemptEmployees || e.OrgHasFullTimeLawyer ||
		e.OrgAssetsOrRevenueCents > exemptAssetsOrRevenueCents
}

// Stage is one step of a staged percentage: the fee is PctPPM of the recovery base when the
// matter resolves at this stage.
type Stage struct {
	Name   string
	PctPPM money.PPM
}

// Agreement is a contingency fee agreement on one matter. It carries no client data.
type Agreement struct {
	ID           string
	MatterID     string
	PracticeArea PracticeArea
	// Paralegal is true when the licensee on the matter is a paralegal (BIL-L-026).
	Paralegal bool
	Kind      Kind
	// SignedDocID links the signed written agreement (BIL-L-015).
	SignedDocID string
	// Form is the form identifier the agreement was drafted on (BIL-L-017).
	Form      string
	Exemption *Exemption
	// Statements maps a prescribed statement number (1..8) to the clause reference a lawyer
	// ticked it against (BIL-L-018).
	Statements map[int]string
	// ClauseTags are the tags of every clause in the agreement (BIL-L-019).
	ClauseTags []string
	// ClientUnderDisability is true for a party under disability with a litigation guardian.
	ClientUnderDisability bool
	JudgeApprovalDocID    string
	ReviewUnderR708       bool
	Stages                []Stage
	// InterimBillsAllowed says the agreement allows an interim fee bill (BIL-C-032).
	InterimBillsAllowed bool
}

// Recovery is what the client recovers from the other side, as stored on the matter's
// settlement record. Disbursements and taxes are the amounts included in the recovery that are
// designated as such (BIL-L-021).
type Recovery struct {
	DamagesCents       money.Cents
	CostsCents         money.Cents
	DisbursementsCents money.Cents
	TaxesCents         money.Cents
}

// Deadlines are the client's (and, for an over-maximum agreement, the lawyer's) last days to
// apply to assess a contingency bill (BIL-L-024). AfterPayment is nil until the bill is paid.
type Deadlines struct {
	AfterDelivery clock.Date
	AfterPayment  *clock.Date
}

// Agreements checks and computes contingency fee agreements. It is pure: callers load and store.
type Agreements struct {
	Flags flags.Flags
	Money money.Arithmetic
}

func (s Agreements) on() error {
	if s.Flags == nil || !s.Flags.Enabled(agreementFlag) {
		return ErrAgreementsOff
	}
	return nil
}

// cfaProblem builds the problem+json error for a CFA refusal.
func cfaProblem(status int, code, rule, title string) error {
	return &api.Problem{Status: status, Code: code, Rule: rule, Title: title}
}

// CheckArrangement is run when a matter's fee arrangement is set to contingency.
func (s Agreements) CheckArrangement(a Agreement) error {
	if err := s.on(); err != nil {
		return err
	}
	// rule: BIL-L-026
	// Nothing below branches on a.Paralegal: paralegal matters get the same checks.
	// rule: BIL-L-016
	switch a.PracticeArea {
	case AreaCriminal, AreaQuasiCriminal, AreaFamily:
		return cfaProblem(http.StatusUnprocessableEntity, "CONTINGENCY_NOT_PERMITTED", "BIL-L-016",
			"contingency fees are not permitted in this practice area")
	}
	// rule: BIL-L-015
	if a.SignedDocID == "" {
		return cfaProblem(http.StatusUnprocessableEntity, "CFA_DOCUMENT_REQUIRED", "BIL-L-015",
			"a signed written contingency fee agreement is required")
	}
	return nil
}

// Activate checks that a CFA may be made active. It runs CheckArrangement first.
func (s Agreements) Activate(a Agreement) error {
	if err := s.CheckArrangement(a); err != nil {
		return err
	}
	// rule: BIL-L-019
	for _, tag := range []string{TagConsentToSettle, TagNoTermination, TagFeeSplitNonLicensee} {
		if slices.Contains(a.ClauseTags, tag) {
			return cfaProblem(http.StatusUnprocessableEntity, "CFA_IMPERMISSIBLE_TERM", "BIL-L-019",
				"the agreement contains a term a contingency fee agreement may not contain")
		}
	}
	switch a.Kind {
	case KindPercentage:
		// rule: BIL-L-017 (verify)
		if a.Form != StandardForm && !a.Exemption.exempt() {
			return cfaProblem(http.StatusUnprocessableEntity, "CFA_STANDARD_FORM_REQUIRED", "BIL-L-017",
				"a percentage contingency fee agreement must use the LSO standard form")
		}
		if len(a.Stages) == 0 {
			return cfaProblem(http.StatusUnprocessableEntity, "CFA_PERCENTAGE_REQUIRED", "BIL-C-032",
				"a percentage contingency fee agreement needs at least one percentage")
		}
	case KindNonPercentage:
		// rule: BIL-L-018
		if !a.Exemption.exempt() {
			for n := 1; n <= StatementCount; n++ {
				if n == StatementDisability && !a.ClientUnderDisability {
					continue
				}
				if a.Statements[n] == "" {
					return cfaProblem(http.StatusUnprocessableEntity, fmt.Sprintf("CFA_MISSING_STATEMENT_%d", n),
						"BIL-L-018", "a prescribed statement is not ticked with a clause reference")
				}
			}
		}
	default:
		return cfaProblem(http.StatusUnprocessableEntity, "CFA_KIND_UNKNOWN", "BIL-C-032",
			"unknown contingency fee agreement kind")
	}
	// rule: BIL-L-025
	if a.ClientUnderDisability && a.JudgeApprovalDocID == "" && !a.ReviewUnderR708 {
		return cfaProblem(http.StatusUnprocessableEntity, "CFA_DISABILITY_APPROVAL_REQUIRED", "BIL-L-025",
			"an agreement for a party under disability needs judge approval or r.7.08 review")
	}
	return nil
}

// RecoveryBase returns damages + costs - designated disbursements - taxes. It is the base of
// the fee and of the cap.
func (s Agreements) RecoveryBase(r Recovery) (money.Cents, error) {
	if err := s.on(); err != nil {
		return 0, err
	}
	// rule: BIL-C-032
	// rule: BIL-L-020
	// rule: BIL-L-021
	base := int64(r.DamagesCents) + int64(r.CostsCents) - int64(r.DisbursementsCents) - int64(r.TaxesCents)
	if r.DamagesCents < 0 || r.CostsCents < 0 || r.DisbursementsCents < 0 || r.TaxesCents < 0 || base < 0 {
		return 0, cfaProblem(http.StatusUnprocessableEntity, "CFA_BAD_RECOVERY", "BIL-C-032",
			"recovery amounts must be non-negative and cover disbursements and taxes")
	}
	return money.Cents(base), nil
}

// StagePct returns the percentage for the named stage of a.
func (s Agreements) StagePct(a Agreement, stage string) (money.PPM, error) {
	if err := s.on(); err != nil {
		return 0, err
	}
	// rule: BIL-C-032
	for _, st := range a.Stages {
		if st.Name == stage {
			return st.PctPPM, nil
		}
	}
	return 0, cfaProblem(http.StatusUnprocessableEntity, "CFA_UNKNOWN_STAGE", "BIL-C-032",
		"the agreement has no percentage for this stage")
}

// Fee returns rhu(base * pct, 1e6).
func (s Agreements) Fee(base money.Cents, pct money.PPM) (money.Cents, error) {
	if err := s.on(); err != nil {
		return 0, err
	}
	if base < 0 || pct < 0 || pct > money.PPM(money.PPMScale) {
		return 0, cfaProblem(http.StatusUnprocessableEntity, "CFA_BAD_PERCENTAGE", "BIL-C-032",
			"base must be non-negative and the percentage between 0 and 100")
	}
	if pct != 0 && int64(base) > math.MaxInt64/int64(pct) {
		return 0, fmt.Errorf("contingency fee: %w", money.ErrOverflow)
	}
	// rule: BIL-C-032
	fee, err := s.Money.Rhu(int64(base)*int64(pct), money.PPMScale)
	if err != nil {
		return 0, fmt.Errorf("contingency fee: %w", err)
	}
	return money.Cents(fee), nil
}

// CheckCap refuses approval of a contingency bill whose fee, excluding disbursements and taxes,
// exceeds the recovery base. On refusal it also returns the capped proposal (the base).
func (s Agreements) CheckCap(fee, base money.Cents) (money.Cents, error) {
	if err := s.on(); err != nil {
		return 0, err
	}
	// rule: BIL-C-033
	// rule: BIL-L-020
	if fee > base {
		return base, cfaProblem(http.StatusConflict, "CFA_FEE_EXCEEDS_RECOVERY", "BIL-C-033",
			"the contingency fee exceeds what the client recovers")
	}
	return fee, nil
}

// CheckInterimBill refuses an interim fee bill unless the agreement allows one.
func (s Agreements) CheckInterimBill(a Agreement) error {
	if err := s.on(); err != nil {
		return err
	}
	// rule: BIL-C-032
	if !a.InterimBillsAllowed {
		return cfaProblem(http.StatusConflict, "CFA_INTERIM_BILL_NOT_ALLOWED", "BIL-C-032",
			"the agreement does not allow an interim fee bill")
	}
	return nil
}

// CheckFeeFromSettlement refuses paying any fee from the settlement of a party under disability
// until the r.7.08 order is linked.
func (s Agreements) CheckFeeFromSettlement(a Agreement, r708OrderDocID string) error {
	if err := s.on(); err != nil {
		return err
	}
	// rule: BIL-L-025
	if a.ClientUnderDisability && r708OrderDocID == "" {
		return cfaProblem(http.StatusConflict, "R708_ORDER_REQUIRED", "BIL-L-025",
			"a fee may not be paid from this settlement until the r.7.08 order is linked")
	}
	return nil
}

// AssessmentDeadlines returns the assessment deadlines of a contingency bill delivered on
// delivered and paid on paid (nil if unpaid). overMaximum is a court-approved agreement above
// the maximum (s.28.1(6)): six months after delivery, for client or lawyer.
func (s Agreements) AssessmentDeadlines(delivered clock.Date, paid *clock.Date, overMaximum bool) (Deadlines, error) {
	if err := s.on(); err != nil {
		return Deadlines{}, err
	}
	// rule: BIL-L-024
	if overMaximum {
		return Deadlines{AfterDelivery: addDate(delivered, 0, 6, 0)}, nil
	}
	d := Deadlines{AfterDelivery: addDate(delivered, 0, 0, 30)}
	if paid != nil {
		p := addDate(*paid, 1, 0, 0)
		d.AfterPayment = &p
	}
	return d, nil
}

// addDate adds calendar years, months and days to a civil date. Days are counted, never
// converted to months.
func addDate(d clock.Date, years, months, days int) clock.Date {
	t := time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, time.UTC).AddDate(years, months, days)
	return clock.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
}
