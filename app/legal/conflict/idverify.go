package conflict

import (
	"context"
	"errors"
	"fmt"

	"github.com/dagi377/counsel/app/legal/legalflags"
)

// FlagIDVerification turns the client ID verification check on (legalflags.IDVerification).
const FlagIDVerification = legalflags.IDVerification

// CodeIDVerificationRequired is the problem+json code for a blocked trust movement.
const CodeIDVerificationRequired = "ID_VERIFICATION_REQUIRED"

// FundsKind is the kind of trust money movement being checked.
type FundsKind string

const (
	FundsReceipt          FundsKind = "receipt"            // money received into trust for the client
	FundsThirdPartyPayout FundsKind = "third_party_payout" // trust money paid to someone other than the firm
	FundsFeeTransfer      FundsKind = "fee_transfer"       // trust money moved to operating for fees or disbursements
)

// Exemption is a reason the verification duty does not apply to one movement.
type Exemption string

const (
	ExemptNone                 Exemption = ""
	ExemptProfessionalFees     Exemption = "professional_fees" // fees, disbursements or expenses
	ExemptBail                 Exemption = "bail"
	ExemptFinancialInstitution Exemption = "financial_institution"
	ExemptPublicBody           Exemption = "public_body"
)

// Funds is one trust movement for a client.
type Funds struct {
	ClientID  string
	Kind      FundsKind
	Exemption Exemption
}

// IDDecision is the result of CheckFunds. Warn means the movement may go ahead but must be
// flagged for review; a blocked movement returns ErrIDNotVerified instead.
type IDDecision struct {
	Warn bool
	Rule string
}

// ErrUnknownFunds is returned for a movement with an unknown kind or exemption, or no client.
var ErrUnknownFunds = errors.New("conflict: unknown funds movement")

// ErrUnverified is returned by CheckFunds while the conflict.id_verification flag is off.
// The check fails closed: until the rule is confirmed no trust movement passes unchecked.
var ErrUnverified = errors.New("conflict: id verification rules not verified")

// CheckFunds applies the client ID verification duty to a trust movement. With the
// conflict.id_verification flag off it refuses every movement with ErrUnverified, the same
// as withdraw.Validate; the exemptions are the unconfirmed detail and apply only when on.
//
// rule: BIL-L-043 (verify)
func CheckFunds(ctx context.Context, v IDVerifier, f Funds) (IDDecision, error) {
	if f.ClientID == "" || !knownKind(f.Kind) || !knownExemption(f.Exemption) {
		return IDDecision{}, ErrUnknownFunds
	}
	// rule: BIL-L-043 (verify)
	if !legalflags.Enabled(ctx, FlagIDVerification) {
		return IDDecision{Rule: "BIL-L-043"}, fmt.Errorf("conflict: %s: %w", CodeIDVerificationRequired, ErrUnverified)
	}
	// rule: BIL-L-043 (verify)
	if f.Kind == FundsFeeTransfer || f.Exemption != ExemptNone {
		return IDDecision{}, nil
	}
	err := v.RequireVerified(ctx, f.ClientID)
	switch {
	case err == nil:
		return IDDecision{}, nil
	case !errors.Is(err, ErrIDNotVerified):
		return IDDecision{}, fmt.Errorf("conflict: check id verification: %w", err)
	}
	// rule: BIL-L-043 (verify)
	if f.Kind == FundsThirdPartyPayout {
		return IDDecision{Rule: "BIL-L-043"}, fmt.Errorf("conflict: %s: %w", CodeIDVerificationRequired, ErrIDNotVerified)
	}
	return IDDecision{Warn: true, Rule: "BIL-L-043"}, nil
}

func knownKind(k FundsKind) bool {
	switch k {
	case FundsReceipt, FundsThirdPartyPayout, FundsFeeTransfer:
		return true
	}
	return false
}

func knownExemption(e Exemption) bool {
	switch e {
	case ExemptNone, ExemptProfessionalFees, ExemptBail, ExemptFinancialInstitution, ExemptPublicBody:
		return true
	}
	return false
}
