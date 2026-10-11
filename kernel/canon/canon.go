// Package canon writes the RFC 8785 (JSON Canonicalization Scheme) form of a decoded JSON
// value. It is the one canonical JSON writer in the kernel: gate payload hashes and AI
// fixture prompt hashes both use it.
//
// Numbers are integer-only. RFC 8785 serialises numbers as ECMAScript 6 does; for an
// integer within +-(2^53-1) that form is the plain decimal, so the writer needs no floating
// point. Any other number (a fraction, an exponent, or an integer outside that range) is
// ErrNumber.
//
// MarshalLiteral is a second, literal-number mode for hashes over free text that may hold
// decimals (AI fixture prompt hashes). It keeps a number's own digits, normalised (sign of
// zero, leading and trailing zeros, exponent form), and never parses it as a float. Gate
// payloads stay on the integer-only Marshal.
package canon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxSafeInt is 2^53-1, the largest integer an IEEE 754 double holds exactly (I-JSON).
const MaxSafeInt = 1<<53 - 1

var (
	// ErrNumber is returned for a number that is not an integer literal within
	// +-MaxSafeInt. "500.0" and "5e4" are rejected although they are integral.
	ErrNumber = errors.New("canon: number is not an integer within +-(2^53-1)")
	// ErrSyntax is returned when the input is not exactly one JSON value.
	ErrSyntax = errors.New("canon: not a single JSON value")
	// ErrType is returned for a Go value that is not a decoded JSON value.
	ErrType = errors.New("canon: unsupported value type")
)

// Decode decodes exactly one JSON value, keeping numbers as json.Number literals.
func Decode(raw []byte) (any, error) {
	var doc any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode: %w", ErrSyntax)
	}
	if _, err := dec.Token(); err == nil {
		return nil, fmt.Errorf("decode: trailing data: %w", ErrSyntax)
	}
	return doc, nil
}

// Marshal returns the canonical form of v, a value as Decode returns it: nil, bool,
// string, json.Number, []any or map[string]any.
func Marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := Write(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// MarshalLiteral is Marshal with numbers written by LiteralNumber instead of Number: any
// JSON number is accepted and written as its normalised literal. Use it only for hashes
// that are not gate payloads.
func MarshalLiteral(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := write(&b, v, LiteralNumber); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Write appends the canonical form of v to b. Object keys are sorted by UTF-16 code unit
// (RFC 8785 sec. 3.2.3), there is no insignificant whitespace, and strings are escaped
// only where sec. 3.2.2.2 requires.
func Write(b *bytes.Buffer, v any) error {
	return write(b, v, Number)
}

func write(b *bytes.Buffer, v any, number func(json.Number) (string, error)) error {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case json.Number:
		s, err := number(t)
		if err != nil {
			return err
		}
		b.WriteString(s)
	case string:
		writeString(b, t)
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := write(b, e, number); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, compareUTF16)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, k)
			b.WriteByte(':')
			if err := write(b, t[k], number); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("value %T: %w", v, ErrType)
	}
	return nil
}

// Number returns the canonical form of an integer literal: its decimal digits, with -0
// written as 0. A fraction, an exponent or a value outside +-MaxSafeInt is ErrNumber.
func Number(n json.Number) (string, error) {
	s := string(n)
	if strings.ContainsAny(s, ".eE") {
		return "", fmt.Errorf("number %s: %w", s, ErrNumber)
	}
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil || i > MaxSafeInt || i < -MaxSafeInt {
		return "", fmt.Errorf("number %s: %w", s, ErrNumber)
	}
	return strconv.FormatInt(i, 10), nil
}

// maxLiteralExp bounds a literal's decimal exponent so position arithmetic cannot overflow.
const maxLiteralExp = 1 << 30

// LiteralNumber returns the normalised text of a JSON number literal without parsing it as a
// float, so no digit is rounded away. The value is written as ECMAScript writes a number
// with exactly these significant digits: plain decimal when the decimal point falls within
// 21 digits of the first digit and no more than 6 places after it, else d.ddde+x / d.ddde-x.
// So 1.0, 1e0 and 10e-1 are all "1", -0 is "0", 0.50 is "0.5" and 1E21 is "1e+21". A
// string that is not a JSON number is ErrNumber.
func LiteralNumber(n json.Number) (string, error) {
	s := string(n)
	if !json.Valid([]byte(s)) || s == "" || (s[0] != '-' && (s[0] < '0' || s[0] > '9')) {
		return "", fmt.Errorf("number %q: %w", s, ErrNumber)
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	mant, exp := s, 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mant = s[:i]
		e, err := strconv.Atoi(strings.TrimPrefix(s[i+1:], "+"))
		if err != nil || e > maxLiteralExp || e < -maxLiteralExp {
			return "", fmt.Errorf("number %q: %w", n, ErrNumber)
		}
		exp = e
	}
	intPart, frac, _ := strings.Cut(mant, ".")
	digits := strings.TrimLeft(intPart+frac, "0")
	// point is where the decimal point sits after the first significant digit is placed at 0.
	point := len(intPart) - (len(intPart+frac) - len(digits)) + exp
	digits = strings.TrimRight(digits, "0")
	if digits == "" {
		return "0", nil
	}
	var out string
	k := len(digits)
	switch {
	case k <= point && point <= 21:
		out = digits + strings.Repeat("0", point-k)
	case 0 < point && point <= 21:
		out = digits[:point] + "." + digits[point:]
	case -6 < point && point <= 0:
		out = "0." + strings.Repeat("0", -point) + digits
	default:
		out = digits[:1]
		if k > 1 {
			out += "." + digits[1:]
		}
		e := point - 1
		if e >= 0 {
			out += "e+" + strconv.Itoa(e)
		} else {
			out += "e-" + strconv.Itoa(-e)
		}
	}
	if neg {
		out = "-" + out
	}
	return out, nil
}

// writeString writes s as an RFC 8785 string (sec. 3.2.2.2): only '"', '\\' and
// U+0000..U+001F are escaped, with \b \t \n \f \r short forms and lowercase \u00xx
// otherwise. Everything else, U+2028 and U+2029 included, is written as UTF-8.
func writeString(b *bytes.Buffer, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20:
			b.WriteString(`\u00`)
			b.WriteByte(hex[r>>4])
			b.WriteByte(hex[r&0xF])
		default:
			b.WriteRune(r) // an invalid byte is written as U+FFFD
		}
		i += size
	}
	b.WriteByte('"')
}

// compareUTF16 orders strings by their UTF-16 code units (RFC 8785 sec. 3.2.3).
func compareUTF16(a, b string) int {
	return slices.Compare(utf16Units(a), utf16Units(b))
}

func utf16Units(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
			continue
		}
		out = append(out, uint16(r))
	}
	return out
}
