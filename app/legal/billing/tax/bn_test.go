package tax

import (
	"errors"
	"strings"
	"testing"
)

// rule: BIL-T-061
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
			err := BN{}.CheckBN(tt.bn)
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
			err := CheckApproval(tt.reg, tt.confirmed)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("CheckApproval(%q, %v) = %v, want %v", tt.reg, tt.confirmed, err, tt.want)
			}
		})
	}
}
