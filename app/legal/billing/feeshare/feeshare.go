package feeshare

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/money"
)

// feeshareFlag is legalflags.ContingencyFees (V-CFA). Every Checker method refuses with
// ErrOff while it is off.
const feeshareFlag = legalflags.ContingencyFees

// ErrOff is returned by every Checker method while billing.contingency_fees is off.
var ErrOff = errors.New("feeshare: fee splitting and referral fees are not enabled")

// Referral fee caps (BIL-L-031). (verify) caps and conditions with the LSO.
const (
	referralTierCents  money.Cents = 5_000_000 // first $50,000 of fees
	referralTier1PPM   money.PPM   = 150_000   // 15% of the first tier
	referralTier2PPM   money.PPM   = 50_000    // 5% thereafter
	referralMaxPerCase money.Cents = 2_500_000 // $25,000 per matter
)

// Kind says what a payable is for.
type Kind string

// Payable kinds. FeeDivision and Referral are calculated as a share of fees or as a referral
// reward; Ordinary is any other payable (a fixed-price vendor invoice).
const (
	KindFeeDivision Kind = "fee_division"
	KindReferral    Kind = "referral"
	KindOrdinary    Kind = "ordinary"
)

// Payee is who a payable goes to. LicenceNumber is the LSO licence number; empty means the
// payee is not a licensee. AffiliationID is an approved MDP or affiliation record.
type Payee struct {
	ID            string
	LicenceNumber string
	AffiliationID string
	// OutsideFirm is true for a licensee who is not a member of this firm.
	OutsideFirm bool
}

// Payable is a payable to be recorded against a matter. It carries no client data.
type Payable struct {
	MatterID    string
	Kind        Kind
	Payee       Payee
	AmountCents money.Cents
	// ClientConsentDocID links the client's written consent.
	ClientConsentDocID string
	// ReferralAgreementDocID links the written referral agreement.
	ReferralAgreementDocID string
	// ConflictReferral is true when the referral is made because of a conflict of interest.
	ConflictReferral bool
}

// Checker checks fee-division and referral-fee payables. It is pure: callers load and store.
type Checker struct {
	Money money.Arithmetic
}

func (c Checker) on(ctx context.Context) error {
	if !legalflags.Enabled(ctx, feeshareFlag) {
		return ErrOff
	}
	return nil
}

func problem(code, rule, title string) error {
	return &api.Problem{Status: http.StatusUnprocessableEntity, Code: code, Rule: rule, Title: title}
}

// CheckPayable refuses a payable that splits fees or pays a referral fee in breach of the
// conduct rules. feesBilledCents is the fees billed on the matter, the base of the referral cap.
func (c Checker) CheckPayable(ctx context.Context, p Payable, feesBilledCents money.Cents) error {
	if err := c.on(ctx); err != nil {
		return err
	}
	if p.AmountCents < 0 {
		return problem("PAYABLE_NEGATIVE", "BIL-L-030", "a payable amount may not be negative")
	}
	if p.Kind == KindOrdinary {
		return nil
	}
	if p.Kind != KindFeeDivision && p.Kind != KindReferral {
		return problem("PAYABLE_KIND_UNKNOWN", "BIL-L-032", "unknown payable kind")
	}
	// rule: BIL-L-032 (verify)
	if p.Payee.LicenceNumber == "" && p.Payee.AffiliationID == "" {
		return problem("FEE_SPLIT_NON_LICENSEE", "BIL-L-032",
			"fees may not be split with, or referral fees paid to, a non-licensee")
	}
	if p.Kind == KindFeeDivision {
		// rule: BIL-L-030 (verify)
		if p.Payee.OutsideFirm && (p.ClientConsentDocID == "" || p.Payee.LicenceNumber == "") {
			return problem("FEE_DIVISION_CONSENT_REQUIRED", "BIL-L-030",
				"a fee division with an outside licensee needs client consent and the licence number")
		}
		return nil
	}
	// rule: BIL-L-031 (verify)
	switch {
	case p.Payee.LicenceNumber == "":
		return problem("FEE_SPLIT_NON_LICENSEE", "BIL-L-031", "a referral fee may be paid only to a licensee")
	case p.ConflictReferral:
		return problem("REFERRAL_CONFLICT", "BIL-L-031", "no referral fee for a referral made because of a conflict")
	case p.ReferralAgreementDocID == "":
		return problem("REFERRAL_AGREEMENT_REQUIRED", "BIL-L-031", "a written referral agreement is required")
	case p.ClientConsentDocID == "":
		return problem("REFERRAL_CONSENT_REQUIRED", "BIL-L-031", "the client's written consent is required")
	}
	limit, err := c.ReferralCap(ctx, feesBilledCents)
	if err != nil {
		return err
	}
	if p.AmountCents > limit {
		return problem("REFERRAL_CAP", "BIL-L-031", "the referral fee exceeds the cap")
	}
	return nil
}

// ReferralCap returns min(15% of the first $50,000 of fees + 5% of the rest, $25,000).
func (c Checker) ReferralCap(ctx context.Context, feesBilledCents money.Cents) (money.Cents, error) {
	if err := c.on(ctx); err != nil {
		return 0, err
	}
	if feesBilledCents <= 0 {
		return 0, nil
	}
	// rule: BIL-L-031 (verify)
	first := min(feesBilledCents, referralTierCents)
	rest := feesBilledCents - first
	a, err := c.Money.Rhu(int64(first)*int64(referralTier1PPM), money.PPMScale)
	if err != nil {
		return 0, fmt.Errorf("referral cap: %w", err)
	}
	// The cap tops out at $25,000, reached at $450,000 of fees; clamp before multiplying.
	rest = min(rest, referralMaxPerCase*money.Cents(money.PPMScale)/money.Cents(referralTier2PPM))
	b, err := c.Money.Rhu(int64(rest)*int64(referralTier2PPM), money.PPMScale)
	if err != nil {
		return 0, fmt.Errorf("referral cap: %w", err)
	}
	return min(money.Cents(a+b), referralMaxPerCase), nil
}
