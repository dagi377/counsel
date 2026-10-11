package gate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxSafeInt is 2^53-1, the largest integer an IEEE 754 double holds exactly (I-JSON).
const maxSafeInt = 1<<53 - 1

// rule: ARC-I-015
// JCS returns the RFC 8785 canonical form of payload: object keys sorted by UTF-16 code
// unit, no insignificant whitespace, strings escaped only where RFC 8785 requires and
// numbers serialised per ECMAScript 6. On top of RFC 8785, a gate payload number must be
// an integer literal within +-(2^53-1), since amounts are integer cents and rates integer
// ppm; any other number is ErrInvalidPayload. PayloadHash is the SHA-256 of these bytes.
func JCS(payload json.RawMessage) ([]byte, error) {
	doc, err := decodeJSON(payload)
	if err != nil {
		return nil, err
	}
	if err := integersOnly(doc); err != nil {
		return nil, err
	}
	return canonicalize(doc)
}

// decodeJSON decodes one JSON value, keeping numbers as their literal text.
func decodeJSON(payload json.RawMessage) (any, error) {
	var doc any
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("payload: %w", ErrInvalidPayload)
	}
	if _, err := dec.Token(); err == nil {
		return nil, fmt.Errorf("payload: trailing data: %w", ErrInvalidPayload)
	}
	return doc, nil
}

// integersOnly rejects any number in doc that is not a plain integer literal within the
// range a double holds exactly. "500.0" and "5e4" are rejected although they are integral.
func integersOnly(doc any) error {
	switch t := doc.(type) {
	case json.Number:
		if strings.ContainsAny(string(t), ".eE") {
			return fmt.Errorf("payload number %s: %w", t, ErrInvalidPayload)
		}
		i, err := t.Int64()
		if err != nil || i > maxSafeInt || i < -maxSafeInt {
			return fmt.Errorf("payload number %s: %w", t, ErrInvalidPayload)
		}
	case []any:
		for _, e := range t {
			if err := integersOnly(e); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, e := range t {
			if err := integersOnly(e); err != nil {
				return err
			}
		}
	}
	return nil
}

// canonicalize serialises a decoded JSON value per RFC 8785.
func canonicalize(doc any) ([]byte, error) {
	var b bytes.Buffer
	if err := writeCanonical(&b, doc); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func writeCanonical(b *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil {
			return fmt.Errorf("payload number %s: %w", t, ErrInvalidPayload)
		}
		s, err := es6Number(f)
		if err != nil {
			return err
		}
		b.WriteString(s)
	case string:
		writeJCSString(b, t)
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, e); err != nil {
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
			writeJCSString(b, k)
			b.WriteByte(':')
			if err := writeCanonical(b, t[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("payload value %T: %w", v, ErrInvalidPayload)
	}
	return nil
}

// es6Number formats f as ECMAScript 6 Number.prototype.toString does (RFC 8785 sec.
// 3.2.2.3): shortest round-trip digits, plain notation for 1e-6 <= |f| < 1e21, otherwise
// exponent notation without exponent zero padding. -0 is "0"; NaN and Infinity are errors.
func es6Number(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("payload number %v: %w", f, ErrInvalidPayload)
	}
	if f == 0 {
		return "0", nil
	}
	if abs := math.Abs(f); abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	}
	// Go pads the exponent to two digits ("1e-07"); ES6 does not ("1e-7").
	mant, exp, _ := strings.Cut(strconv.FormatFloat(f, 'e', -1, 64), "e")
	return mant + "e" + exp[:1] + strings.TrimLeft(exp[1:], "0"), nil
}

// writeJCSString writes s as an RFC 8785 string (sec. 3.2.2.2): only '"', '\\' and
// U+0000..U+001F are escaped, with \b \t \n \f \r short forms and lowercase \u00xx
// otherwise. Everything else, U+2028 and U+2029 included, is written as UTF-8.
func writeJCSString(b *bytes.Buffer, s string) {
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
