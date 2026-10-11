package withdraw

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/trust"
)

// FlagBylaw9Verified gates every check here built on unconfirmed By-Law 9 text. It is
// legalflags.TrustBylaw9Verified.
const FlagBylaw9Verified = legalflags.TrustBylaw9Verified

var (
	// ErrUnverified is returned while FlagBylaw9Verified is off: no withdrawal is accepted
	// until the By-Law 9 rules behind it are confirmed.
	ErrUnverified = errors.New("withdraw: by-law 9 rules not verified")
	// ErrBillNotDelivered is returned for a fee withdrawal whose bill is not delivered (409).
	ErrBillNotDelivered = errors.New("withdraw: bill not delivered")
	// ErrExceedsBillBalance is returned for a fee withdrawal above the bill's balance due (409).
	ErrExceedsBillBalance = errors.New("withdraw: exceeds bill balance")
	// ErrSignerNotAuthorized is returned when the authorizer has no trust signing authority.
	ErrSignerNotAuthorized = errors.New("withdraw: signer not authorized")
)

// Funds is one matter ledger's trust position used to validate a withdrawal. All cents.
type Funds struct {
	ClearedCents    int64 // posted and cleared on or before today
	UnclearedCents  int64 // receipts posted but not yet cleared
	ProtectedCents  int64
	MinimumCents    int64
	AuthorizedCents int64 // withdrawals and fee transfers authorized but not executed
}

// Available is the amount that may still be withdrawn: cleared minus protected, minimum and
// authorised-not-executed holds, computed by trust.AvailableCents, the one available-balance
// formula.
func (f Funds) Available() int64 {
	// rule: ARC-I-025
	return trust.AvailableCents(f.ClearedCents, f.ProtectedCents, f.MinimumCents, f.AuthorizedCents)
}

// FeeBill is the bill a fee withdrawal pays. A zero DeliveredAt means not delivered.
type FeeBill struct {
	DeliveredAt     time.Time
	BalanceDueCents int64
}

// Signer is the user who authorizes a withdrawal.
type Signer struct {
	UserID       string
	TrustSigning bool // licensee or approved bonded non-licensee with trust signing authority
}

// Validate checks a withdrawal request against the ledger's funds before it is recorded.
// It returns ErrUnverified while FlagBylaw9Verified is off.
func Validate(ctx context.Context, r Request, f Funds) error {
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return ErrUnverified
	}
	// rule: BIL-L-037 (verify)
	switch p := strings.ToLower(strings.TrimSpace(r.Payee)); p {
	case "cash", "bearer":
		return fmt.Errorf("validate payee: %w", ErrBearerPayee)
	case "":
		return fmt.Errorf("validate payee missing: %w", ErrInvalid)
	}
	if r.Method != MethodCheque && r.Method != MethodEFT {
		return fmt.Errorf("validate method %q: %w", r.Method, ErrBearerPayee)
	}
	if r.AmountCents <= 0 || r.TrustAccountID == "" || r.MatterID == "" || r.PreparedBy == "" {
		return fmt.Errorf("validate fields: %w", ErrInvalid)
	}
	// rule: BIL-L-038 (verify)
	if r.Method == MethodEFT && r.EFTAuthorizationID == "" {
		return fmt.Errorf("validate eft: %w", ErrNoEFTAuthorization)
	}
	return checkFunds(r.AmountCents, f)
}

// checkFunds refuses an amount above the available balance. When the shortfall is covered
// by uncleared receipts the refusal is uncleared_funds (a hard block by default).
func checkFunds(amount int64, f Funds) error {
	// rule: BIL-C-053 (verify)
	avail := f.Available()
	if amount <= avail {
		return nil
	}
	if f.UnclearedCents > 0 && amount <= avail+f.UnclearedCents {
		return fmt.Errorf("check funds: %w", ErrUnclearedFunds)
	}
	return fmt.Errorf("check funds: %w", ErrTrustOverdraw)
}

// ValidateFee checks a withdrawal for fees against the bill it pays, dated on (business date).
func ValidateFee(ctx context.Context, r Request, b FeeBill, on time.Time) error {
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return ErrUnverified
	}
	// rule: BIL-L-035 (verify)
	if b.DeliveredAt.IsZero() || b.DeliveredAt.After(on) {
		return fmt.Errorf("validate fee: %w", ErrBillNotDelivered)
	}
	if r.AmountCents > b.BalanceDueCents {
		return fmt.Errorf("validate fee: %w", ErrExceedsBillBalance)
	}
	return nil
}

// ValidateAuthorizer checks who may authorize w, and re-checks the funds at authorization.
// The agent never reaches here as an authorizer: an agent principal only prepares.
func ValidateAuthorizer(ctx context.Context, w Withdrawal, s Signer, f Funds) error {
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return ErrUnverified
	}
	// rule: ARC-I-019
	if s.UserID == "" || s.UserID == w.Request.PreparedBy {
		return fmt.Errorf("validate authorizer: %w", ErrPreparerApproves)
	}
	// rule: BIL-L-038 (verify)
	if !s.TrustSigning {
		return fmt.Errorf("validate authorizer: %w", ErrSignerNotAuthorized)
	}
	return checkFunds(w.Request.AmountCents, f)
}
