package money

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"
)

var _ Arithmetic = arith{}

// arith adapts the package-level functions to the Arithmetic interface.
type arith struct{}

func (arith) Rhu(num, den int64) (int64, error)                      { return Rhu(num, den) }
func (arith) Allocate(total Cents, weights []int64) ([]Cents, error) { return Allocate(total, weights) }

// Default is the production Arithmetic, backed by Rhu and Allocate.
var Default Arithmetic = arith{}

// uabs returns |v| as uint64; it is exact for math.MinInt64.
func uabs(v int64) uint64 {
	if v < 0 {
		return uint64(^v) + 1
	}
	return uint64(v)
}

// signed turns a magnitude and sign into an int64, or reports overflow.
func signed(mag uint64, neg bool) (int64, error) {
	if neg {
		if mag > uint64(math.MaxInt64)+1 {
			return 0, ErrOverflow
		}
		return int64(-mag), nil
	}
	if mag > math.MaxInt64 {
		return 0, ErrOverflow
	}
	return int64(mag), nil
}

// Rhu returns num/den rounded half away from zero. It is the only division used for
// money: rhu(5,10)=1, rhu(-5,10)=-1, rhu(4,10)=0. It never wraps.
//
// rule: BIL-C-002
func Rhu(num, den int64) (int64, error) {
	if den == 0 {
		return 0, ErrDivideByZero
	}
	// rule: BIL-C-002
	// Work on magnitudes so math.MinInt64 is exact; round half away from zero.
	n, d := uabs(num), uabs(den)
	q, r := n/d, n%d
	if r >= d-r {
		q++
	}
	return signed(q, (num < 0) != (den < 0))
}

// MulRhu returns a*b/den rounded half away from zero, computing the product in 128 bits.
// It returns ErrOverflow when a*b does not fit in int64 or the result does not; it never
// wraps. Use it for cents x ppm: MulRhu(int64(c), int64(rate), PPMScale).
//
// rule: BIL-C-001
func MulRhu(a, b, den int64) (int64, error) {
	if den == 0 {
		return 0, ErrDivideByZero
	}
	hi, lo := bits.Mul64(uabs(a), uabs(b))
	neg := (a < 0) != (b < 0)
	if hi != 0 {
		return 0, ErrOverflow
	}
	p, err := signed(lo, neg)
	if err != nil {
		return 0, err
	}
	return Rhu(p, den)
}

// ApplyPPM returns c x rate / 1e6 rounded half away from zero (one rounding point).
//
// rule: BIL-C-001
func ApplyPPM(c Cents, rate PPM) (Cents, error) {
	v, err := MulRhu(int64(c), int64(rate), PPMScale)
	if err != nil {
		return 0, fmt.Errorf("apply ppm: %w", err)
	}
	return Cents(v), nil
}

// Add returns a+b or ErrOverflow; it never wraps.
func Add(a, b Cents) (Cents, error) {
	s := a + b
	if (a > 0 && b > 0 && s < 0) || (a < 0 && b < 0 && s >= 0) {
		return 0, ErrOverflow
	}
	return s, nil
}

// UnmarshalJSON accepts only a JSON integer. A float (12.5), an exponent or a string
// ("1250") is ErrParse.
//
// rule: ARC-I-003
func (c *Cents) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.ContainsAny(b, ".eE\"") {
		return fmt.Errorf("cents %q: %w", b, ErrParse)
	}
	v, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return fmt.Errorf("cents %q: %w", b, ErrParse)
	}
	*c = Cents(v)
	return nil
}

// MarshalJSON writes the amount as a JSON integer.
func (c Cents) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(int64(c), 10)), nil
}

// UnmarshalJSON accepts only a JSON integer number of parts per million.
//
// rule: BIL-I-024
func (p *PPM) UnmarshalJSON(b []byte) error {
	var c Cents
	if err := c.UnmarshalJSON(b); err != nil {
		return fmt.Errorf("ppm: %w", err)
	}
	*p = PPM(c)
	return nil
}

// ParseDecimal converts a decimal amount in major units ("1,234.56", "-0.1", "1e2") to
// cents without floats. More than two fractional digits after applying the exponent is
// ErrParse; nothing is rounded. Thousands separators are commas.
//
// rule: MIG-005
func ParseDecimal(s string) (Cents, error) {
	orig := s
	bad := func() (Cents, error) { return 0, fmt.Errorf("decimal %q: %w", orig, ErrParse) }
	s = strings.TrimSpace(s)
	neg := false
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	exp := 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		e, err := strconv.Atoi(s[i+1:])
		if err != nil || e < -18 || e > 18 {
			return bad()
		}
		exp, s = e, s[:i]
	}
	intPart, frac, hasDot := strings.Cut(s, ".")
	if hasDot && frac == "" {
		return bad()
	}
	if strings.Contains(intPart, ",") {
		groups := strings.Split(intPart, ",")
		if len(groups[0]) < 1 || len(groups[0]) > 3 {
			return bad()
		}
		for _, g := range groups[1:] {
			if len(g) != 3 {
				return bad()
			}
		}
		intPart = strings.Join(groups, "")
	}
	if intPart == "" && frac == "" {
		return bad()
	}
	digits := intPart + frac
	for _, r := range digits {
		if r < '0' || r > '9' {
			return bad()
		}
	}
	// value = digits x 10^(exp - len(frac)); cents = value x 100.
	shift := exp - len(frac) + 2
	if shift < 0 {
		// Only trailing zeros may be dropped; anything else needs rounding.
		cut := len(digits) + shift
		if cut < 0 {
			cut = 0
		}
		if strings.Trim(digits[cut:], "0") != "" {
			return bad()
		}
		digits = digits[:cut]
		shift = 0
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return 0, nil
	}
	digits += strings.Repeat("0", shift)
	if len(digits) > 19 {
		return 0, fmt.Errorf("decimal %q: %w", orig, ErrOverflow)
	}
	mag, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("decimal %q: %w", orig, ErrOverflow)
	}
	v, err := signed(mag, neg)
	if err != nil {
		return 0, fmt.Errorf("decimal %q: %w", orig, err)
	}
	return Cents(v), nil
}

// ParseJSONNumber converts a json.Number (decoded with UseNumber) to cents via
// ParseDecimal, never through float64.
//
// rule: MIG-005
func ParseJSONNumber(n json.Number) (Cents, error) { return ParseDecimal(n.String()) }
