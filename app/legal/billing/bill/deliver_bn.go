package bill

import (
	"errors"
	"fmt"

	"github.com/dagi377/counsel/app/legal/billing/tax"
)

// CodeHSTNumberMissing is the problem+json code for a delivery blocked by ErrMissingBN.
const CodeHSTNumberMissing = "HST_NUMBER_MISSING"

// ErrMissingBN is returned when a GST/HST registrant firm has no valid BN/RT number to
// print on the bill, so the bill may not be delivered.
var ErrMissingBN = errors.New("bill: firm GST/HST registration number missing")

// ErrNumbering is returned by CheckNumbering when bill numbers break BIL-I-008.
var ErrNumbering = errors.New("bill: numbering invariant broken")

// FirmTax is the firm's GST/HST registration as delivery needs it.
type FirmTax struct {
	Registrant bool   // the firm is a GST/HST registrant
	BN         string // its registration number, e.g. "123456782RT0001"; never logged
}

// CheckDeliverable refuses delivery of b when the firm is a GST/HST registrant without a
// valid BN/RT number. An unregistered firm passes. The number is checked through bn, which
// fails closed: while tax.bn_check_verified is off a registrant's delivery returns
// tax.ErrUnverified. Errors never contain the number.
//
// rule: BIL-L-047 (verify)
func CheckDeliverable(b Bill, firm FirmTax, bn tax.BNChecker) error {
	// rule: BIL-L-047 (verify)
	if !firm.Registrant {
		return nil
	}
	if firm.BN == "" {
		return fmt.Errorf("deliver %s: %s: %w", b.ID, CodeHSTNumberMissing, ErrMissingBN)
	}
	if bn == nil {
		return fmt.Errorf("deliver %s: no bn checker: %w", b.ID, tax.ErrUnverified)
	}
	err := bn.CheckBN(firm.BN)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, tax.ErrBadBN):
		return fmt.Errorf("deliver %s: %s: %w", b.ID, CodeHSTNumberMissing, ErrMissingBN)
	default:
		return fmt.Errorf("deliver %s: check bn: %w", b.ID, err)
	}
}

// CheckNumbering checks the numbering invariant over every bill of the firm: a bill not yet
// approved has no number, an approved or later bill has one (a void keeps the number it
// had), numbers are unique, and each prefix-year series runs without a gap
// (count(distinct n) = max(n) - min(n) + 1).
//
// rule: BIL-I-008
func CheckNumbering(bills []Bill) error {
	type series struct {
		prefix string
		year   int
	}
	seen := map[string]ID{}
	seqs := map[series][]int{}
	for _, b := range bills {
		// rule: BIL-I-008
		switch {
		case editable(b.Status) && b.Number != "":
			return fmt.Errorf("bill %s in %s has number: %w", b.ID, b.Status, ErrNumbering)
		case !editable(b.Status) && b.Status != StatusVoid && b.Number == "":
			return fmt.Errorf("bill %s in %s has no number: %w", b.ID, b.Status, ErrNumbering)
		case b.Number == "":
			continue
		}
		if other, dup := seen[b.Number]; dup {
			return fmt.Errorf("bills %s and %s share number %s: %w", other, b.ID, b.Number, ErrNumbering)
		}
		seen[b.Number] = b.ID
		p, y, s, err := ParseNumber(b.Number)
		if err != nil {
			return fmt.Errorf("bill %s: %w: %w", b.ID, ErrNumbering, err)
		}
		k := series{p, y}
		seqs[k] = append(seqs[k], s)
	}
	for k, ns := range seqs {
		lo, hi := ns[0], ns[0]
		for _, n := range ns {
			lo, hi = min(lo, n), max(hi, n)
		}
		// rule: BIL-I-008
		if len(ns) != hi-lo+1 {
			return fmt.Errorf("series %s-%04d has %d numbers between %d and %d: %w", k.prefix, k.year, len(ns), lo, hi, ErrNumbering)
		}
	}
	return nil
}
