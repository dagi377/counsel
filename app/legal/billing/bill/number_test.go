package bill

import (
	"errors"
	"testing"
)

// rule: BIL-C-120
func TestBIL_C_120_NumberFormat(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		year   int
		seq    int
		want   string
		err    error
	}{
		{"padded to four", "INV", 2026, 147, "INV-2026-0147", nil},
		{"first of year", "INV", 2026, 1, "INV-2026-0001", nil},
		{"four digits exact", "INV", 2026, 9999, "INV-2026-9999", nil},
		{"wider not truncated", "INV", 2026, 10000, "INV-2026-10000", nil},
		{"alphanumeric prefix", "TOR2", 2027, 3, "TOR2-2027-0003", nil},
		{"empty prefix", "", 2026, 1, "", ErrInvalid},
		{"prefix with dash", "IN-V", 2026, 1, "", ErrInvalid},
		{"zero seq", "INV", 2026, 0, "", ErrInvalid},
		{"negative seq", "INV", 2026, -1, "", ErrInvalid},
		{"short year", "INV", 26, 1, "", ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FormatNumber(tc.prefix, tc.year, tc.seq)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if tc.err != nil {
				return
			}
			p, y, s, err := ParseNumber(got)
			if err != nil || p != tc.prefix || y != tc.year || s != tc.seq {
				t.Fatalf("round trip %q = %q %d %d %v", got, p, y, s, err)
			}
		})
	}
}

// rule: BIL-C-120
func TestBIL_C_120_ParseNumberMalformed(t *testing.T) {
	for _, in := range []string{
		"", "INV", "INV-2026", "INV-2026-", "-2026-0001", "INV-26-0001", "INV-2026-001",
		"INV-2026-00a1", "INV-2026-0000", "INV-2026-01000", "INV-20x6-0001", "IN V-2026-0001",
		"INV-2026-+001", "INV--2026-0001",
	} {
		t.Run(in, func(t *testing.T) {
			if _, _, _, err := ParseNumber(in); !errors.Is(err, ErrInvalid) {
				t.Fatalf("ParseNumber(%q) err = %v, want ErrInvalid", in, err)
			}
		})
	}
}
