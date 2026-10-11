package bill

import (
	"fmt"
	"strconv"
	"strings"
)

// numberSeqWidth is the minimum width of the sequence part of a bill number.
const numberSeqWidth = 4

// FormatNumber returns the bill number prefix-YYYY-NNNN. The sequence is zero-padded to
// four digits; a wider sequence is written in full, never truncated.
func FormatNumber(prefix string, year, seq int) (string, error) {
	// rule: BIL-C-120
	if !validPrefix(prefix) || year < 1000 || year > 9999 || seq < 1 {
		return "", fmt.Errorf("format bill number %q/%d/%d: %w", prefix, year, seq, ErrInvalid)
	}
	return fmt.Sprintf("%s-%04d-%0*d", prefix, year, numberSeqWidth, seq), nil
}

// ParseNumber splits a bill number made by FormatNumber into its prefix, year and
// sequence. Malformed input returns ErrInvalid.
func ParseNumber(s string) (prefix string, year, seq int, err error) {
	// rule: BIL-C-120
	bad := fmt.Errorf("parse bill number %q: %w", s, ErrInvalid)
	i := strings.LastIndexByte(s, '-')
	if i < 0 {
		return "", 0, 0, bad
	}
	seqPart := s[i+1:]
	j := strings.LastIndexByte(s[:i], '-')
	if j < 0 {
		return "", 0, 0, bad
	}
	prefix, yearPart := s[:j], s[j+1:i]
	if !validPrefix(prefix) || len(yearPart) != 4 || !digits(yearPart) ||
		len(seqPart) < numberSeqWidth || !digits(seqPart) {
		return "", 0, 0, bad
	}
	// A sequence wider than the padding never starts with zero.
	if len(seqPart) > numberSeqWidth && seqPart[0] == '0' {
		return "", 0, 0, bad
	}
	year, _ = strconv.Atoi(yearPart)
	seq, err = strconv.Atoi(seqPart)
	if err != nil || seq < 1 || year < 1000 {
		return "", 0, 0, bad
	}
	return prefix, year, seq, nil
}

// validPrefix reports whether p is a non-empty series prefix of ASCII letters and digits.
func validPrefix(p string) bool {
	if p == "" {
		return false
	}
	for _, r := range p {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// digits reports whether s is all ASCII digits.
func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
