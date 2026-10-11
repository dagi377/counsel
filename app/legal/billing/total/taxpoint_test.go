package total

import (
	"errors"
	"testing"
	"time"
)

func tor(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, businessZone)
}

// rule: BIL-T-050
func TestBIL_T_050_TaxPointEarlierOfPaidAndDue(t *testing.T) {
	tests := []struct {
		name       string
		in         TaxPointInput
		wantDay    time.Time
		wantPeriod time.Month // monthly filer's period
		wantErr    error
	}{
		{
			name:       "approved 2026-10-31 reports in October",
			in:         TaxPointInput{ApprovedOn: tor(2026, 10, 31, 16, 0)},
			wantDay:    tor(2026, 10, 31, 0, 0),
			wantPeriod: time.October,
		},
		{
			name:       "approved 2026-10-31 late evening, stored UTC on Nov 1, still October",
			in:         TaxPointInput{ApprovedOn: time.Date(2026, 11, 1, 3, 30, 0, 0, time.UTC)},
			wantDay:    tor(2026, 10, 31, 0, 0),
			wantPeriod: time.October,
		},
		{
			name:       "approved 2026-09-30 23:30 Toronto is September",
			in:         TaxPointInput{ApprovedOn: time.Date(2026, 10, 1, 3, 30, 0, 0, time.UTC)},
			wantDay:    tor(2026, 9, 30, 0, 0),
			wantPeriod: time.September,
		},
		{
			name:       "paid before approval: paid day wins",
			in:         TaxPointInput{ApprovedOn: tor(2026, 11, 2, 9, 0), PaidOn: tor(2026, 10, 28, 9, 0)},
			wantDay:    tor(2026, 10, 28, 0, 0),
			wantPeriod: time.October,
		},
		{
			name:       "paid after approval: bill date wins",
			in:         TaxPointInput{ApprovedOn: tor(2026, 10, 31, 9, 0), PaidOn: tor(2026, 11, 15, 9, 0)},
			wantDay:    tor(2026, 10, 31, 0, 0),
			wantPeriod: time.October,
		},
		{
			name:       "contractual due before bill date wins",
			in:         TaxPointInput{ApprovedOn: tor(2026, 11, 3, 9, 0), DueOn: tor(2026, 10, 30, 9, 0)},
			wantDay:    tor(2026, 10, 30, 0, 0),
			wantPeriod: time.October,
		},
		{
			name:       "due later than bill date is capped at the bill date",
			in:         TaxPointInput{ApprovedOn: tor(2026, 10, 31, 9, 0), DueOn: tor(2026, 11, 30, 9, 0)},
			wantDay:    tor(2026, 10, 31, 0, 0),
			wantPeriod: time.October,
		},
		{
			name:       "paid only, not approved",
			in:         TaxPointInput{PaidOn: tor(2026, 10, 5, 9, 0)},
			wantDay:    tor(2026, 10, 5, 0, 0),
			wantPeriod: time.October,
		},
		{
			name:    "neither approved nor paid",
			in:      TaxPointInput{},
			wantErr: ErrNoTaxPoint,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TaxPoint(tc.in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if !got.Equal(tc.wantDay) {
				t.Fatalf("tax point = %v, want %v", got, tc.wantDay)
			}
			if got.In(businessZone).Month() != tc.wantPeriod {
				t.Fatalf("period month = %v, want %v", got.In(businessZone).Month(), tc.wantPeriod)
			}
		})
	}
}
