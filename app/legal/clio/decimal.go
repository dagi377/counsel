package clio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dagi377/counsel/kernel/money"
)

// Currency is the only currency the importer accepts (MIG-005).
const Currency = "CAD"

// DecodeRecord decodes one source record with every JSON number kept as json.Number, so
// money is never parsed through float64.
//
// rule: MIG-005
func DecodeRecord(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode record: %w", err)
	}
	return nil
}

// ParseCents converts a decimal string ("1234.5", "0.1", "1e2", "-3.25") to integer cents.
// A value with more than 2 significant fractional digits is an error, never rounded.
//
// rule: MIG-005
func ParseCents(s string) (money.Cents, error) {
	bad := func(why string) (money.Cents, error) {
		return 0, fmt.Errorf("money %q: %s: %w", s, why, ErrBadDecimal)
	}
	t := strings.TrimSpace(s)
	neg := false
	if t != "" && (t[0] == '-' || t[0] == '+') {
		neg = t[0] == '-'
		t = t[1:]
	}
	exp := 0
	if i := strings.IndexAny(t, "eE"); i >= 0 {
		e, err := strconv.Atoi(t[i+1:])
		if err != nil || e > 30 || e < -30 {
			return bad("bad exponent")
		}
		exp, t = e, t[:i]
	}
	intPart, frac, _ := strings.Cut(t, ".")
	if intPart+frac == "" {
		return bad("no digits")
	}
	digits := intPart + frac
	for _, c := range digits {
		if c < '0' || c > '9' {
			return bad("not a decimal")
		}
	}
	// value = digits * 10^(exp - len(frac)); cents = digits * 10^(exp - len(frac) + 2).
	shift := exp - len(frac) + 2
	digits = strings.TrimLeft(digits, "0")
	for shift < 0 && strings.HasSuffix(digits, "0") {
		digits = digits[:len(digits)-1]
		shift++
	}
	if digits == "" {
		return 0, nil
	}
	if shift < 0 {
		return bad("more than 2 fractional digits")
	}
	if len(digits)+shift > 18 {
		return bad("out of range")
	}
	v, err := strconv.ParseInt(digits+strings.Repeat("0", shift), 10, 64)
	if err != nil {
		return bad("out of range")
	}
	if neg {
		v = -v
	}
	return money.Cents(v), nil
}

// MoneyCents converts a decoded amount and its currency to cents. The amount must be a
// json.Number or a string; any other type (such as a number decoded without UseNumber) is
// rejected. Any currency but CAD is an error.
//
// rule: MIG-005
func MoneyCents(amount any, currency string) (money.Cents, error) {
	if !strings.EqualFold(strings.TrimSpace(currency), Currency) {
		return 0, fmt.Errorf("money currency %q: want %s: %w", currency, Currency, ErrBadDecimal)
	}
	switch a := amount.(type) {
	case json.Number:
		return ParseCents(a.String())
	case string:
		return ParseCents(a)
	}
	return 0, fmt.Errorf("money amount of type %T: %w", amount, ErrBadDecimal)
}
