package tax

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
)

func bnFlags(t *testing.T, on bool) *flags.Set {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: FlagBNCheckVerified, Owner: "test", Expiry: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(FlagBNCheckVerified, on); err != nil {
		t.Fatal(err)
	}
	return s
}

// rule: BIL-T-061 (verify)
func TestBIL_T_061_BusinessNumberCheckDigit(t *testing.T) {
	tests := []struct {
		name string
		bn   string
		want error
	}{
		{"sample number passes Luhn sum 40", "123456782RT0001", nil},
		{"bad check digit", "123456789RT0001", ErrBadBN},
		{"eight digits", "12345678RT0001", ErrBadBN},
		{"wrong program", "123456782RC0001", ErrBadBN},
		{"letter in account", "123456782RT00A1", ErrBadBN},
		{"letter in bn", "12345678ART0001", ErrBadBN},
		{"empty", "", ErrBadBN},
		{"spaces", "123456782 RT0001", ErrBadBN},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := BN{Flags: bnFlags(t, true)}.CheckBN(tt.bn)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("CheckBN(%q) = %v, want %v", tt.bn, err, tt.want)
			}
			if err != nil && tt.bn != "" && strings.Contains(err.Error(), tt.bn) {
				t.Fatalf("error %q leaks the number", err)
			}
		})
	}
}

// rule: BIL-C-201
func TestBIL_C_201_ApprovalRefusedForBadRegistration(t *testing.T) {
	tests := []struct {
		name      string
		reg       string
		confirmed bool
		want      error
	}{
		{"bad check digit refused", "123456789RT0001", true, ErrBadBN},
		{"missing registration", "", true, ErrRegistrationMissing},
		{"place not confirmed", "123456782RT0001", false, ErrPlaceUnconfirmed},
		{"valid and confirmed", "123456782RT0001", true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckApproval(flags.WithSet(context.Background(), bnFlags(t, true)), tt.reg, tt.confirmed)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("CheckApproval(%q, %v) = %v, want %v", tt.reg, tt.confirmed, err, tt.want)
			}
		})
	}
}

// rule: BIL-T-061 (verify)
func TestBIL_T_061_FlagOffRefusesValidNumber(t *testing.T) {
	tests := []struct {
		name string
		bn   string
		want error
	}{
		{"valid number unverified", "123456782RT0001", ErrUnverified},
		{"bad check digit still bad", "123456789RT0001", ErrBadBN},
		{"bad format still bad", "12345678RT0001", ErrBadBN},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for name, err := range map[string]error{
				"BN off":       BN{Flags: bnFlags(t, false)}.CheckBN(tt.bn),
				"BN nil flags": BN{}.CheckBN(tt.bn),
				"CheckBN ctx":  CheckBN(flags.WithSet(context.Background(), bnFlags(t, false)), tt.bn),
				"CheckBN none": CheckBN(context.Background(), tt.bn),
			} {
				if !errors.Is(err, tt.want) {
					t.Fatalf("%s: err = %v, want %v", name, err, tt.want)
				}
			}
		})
	}
	if err := CheckApproval(context.Background(), "123456782RT0001", true); !errors.Is(err, ErrUnverified) {
		t.Fatalf("CheckApproval with flag off = %v, want ErrUnverified", err)
	}
}
