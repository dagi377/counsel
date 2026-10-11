package tax

import (
	"context"
	"errors"
	"fmt"

	"github.com/dagi377/counsel/app/legal/legalflags"
)

// FlagBNCheckVerified gates acceptance of a GST/HST registration number under BIL-T-061. It
// mirrors legal.FlagTaxBNCheckVerified, which this package cannot import. Off by default:
// a malformed number is always refused, a well-formed one returns ErrUnverified.
const FlagBNCheckVerified = legalflags.TaxBNCheckVerified

// Sentinel errors for the tax approval preconditions (BIL-C-201).
var (
	// ErrRegistrationMissing is returned when the firm has no GST/HST registration number
	// (HTTP 409 tax_registration_missing).
	ErrRegistrationMissing = errors.New("tax: registration number missing")
	// ErrPlaceUnconfirmed is returned when the lawyer has not confirmed the place of supply.
	ErrPlaceUnconfirmed = errors.New("tax: place of supply not confirmed")
)

// BN checks GST/HST registration numbers. It implements BNChecker. BNChecker takes no
// context, so BN reads FlagBNCheckVerified from Flags; a nil Flags is off.
type BN struct {
	Flags interface{ Enabled(name string) bool }
}

var _ BNChecker = BN{}

// CheckBN validates bn as 9 digits, "RT" and a 4-digit account, with the 9-digit business
// number passing the Luhn check digit. The error never contains bn.
func (b BN) CheckBN(bn string) error {
	if err := checkBN(bn); err != nil {
		return err
	}
	// rule: BIL-T-061 (verify)
	if b.Flags == nil || !b.Flags.Enabled(FlagBNCheckVerified) {
		return fmt.Errorf("check bn: %w", ErrUnverified)
	}
	return nil
}

// CheckBN validates a GST/HST registration number such as "123456782RT0001". A malformed
// number returns ErrBadBN; a valid one returns ErrUnverified while FlagBNCheckVerified is off.
//
// rule: BIL-T-061 (verify)
func CheckBN(ctx context.Context, bn string) error {
	if err := checkBN(bn); err != nil {
		return err
	}
	// rule: BIL-T-061 (verify)
	if !legalflags.Enabled(ctx, FlagBNCheckVerified) {
		return fmt.Errorf("check bn: %w", ErrUnverified)
	}
	return nil
}

func checkBN(bn string) error {
	// rule: BIL-T-061
	if len(bn) != 15 || bn[9:11] != "RT" || !digits(bn[:9]) || !digits(bn[11:]) {
		return fmt.Errorf("check bn format: %w", ErrBadBN)
	}
	// rule: BIL-T-061
	if !luhn(bn[:9]) {
		return fmt.Errorf("check bn digit: %w", ErrBadBN)
	}
	return nil
}

// CheckApproval is the tax precondition for approving a bill: the firm registration number
// is present and valid, and the lawyer confirmed the place of supply.
func CheckApproval(ctx context.Context, registration string, placeConfirmed bool) error {
	// rule: BIL-C-201
	if registration == "" {
		return fmt.Errorf("approval: %w", ErrRegistrationMissing)
	}
	// rule: BIL-C-201
	if err := CheckBN(ctx, registration); err != nil {
		return fmt.Errorf("approval: %w", err)
	}
	// rule: BIL-C-201
	if !placeConfirmed {
		return fmt.Errorf("approval: %w", ErrPlaceUnconfirmed)
	}
	return nil
}

func digits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// luhn reports whether s (ASCII digits) passes the Luhn check: doubling every second digit
// from the right, the digit sum is a multiple of 10.
func luhn(s string) bool {
	sum := 0
	for i := 0; i < len(s); i++ {
		d := int(s[len(s)-1-i] - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}
