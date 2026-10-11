// Package canon writes the RFC 8785 (JSON Canonicalization Scheme) form of a decoded JSON
// value. It is the one canonical JSON writer in the kernel: gate payload hashes and AI
// fixture prompt hashes both use it.
//
// Numbers are integer-only. RFC 8785 serialises numbers as ECMAScript 6 does; for an
// integer within +-(2^53-1) that form is the plain decimal, so the writer needs no floating
// point. Any other number (a fraction, an exponent, or an integer outside that range) is
// ErrNumber.
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

// Write appends the canonical form of v to b. Object keys are sorted by UTF-16 code unit
// (RFC 8785 sec. 3.2.3), there is no insignificant whitespace, and strings are escaped
// only where sec. 3.2.2.2 requires.
func Write(b *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case json.Number:
		s, err := Number(t)
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
			if err := Write(b, e); err != nil {
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
			if err := Write(b, t[k]); err != nil {
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
