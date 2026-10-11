package assessment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/interest/interestfake"
	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
)

func requisitionFixture(t *testing.T, today string, flagOn bool) (*Requisitions, *time.Location, context.Context) {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	now, err := time.ParseInLocation(time.DateOnly, today, loc)
	if err != nil {
		t.Fatal(err)
	}
	ctx := legalflagstest.Context(t, FlagSolicitorsActVerified, flagOn)
	return &Requisitions{Calendar: interestfake.New(loc), Clock: clockfake.New(now.Add(12*time.Hour), loc)}, loc, ctx
}

// rule: BIL-L-005
func TestBIL_L_005_RequisitionWindowAndHold(t *testing.T) {
	tests := []struct {
		name       string
		today      string
		note       string
		flagOn     bool
		wantErr    error
		wantStatus string
	}{
		{"within one month stops reminders and interest", "2026-04-15", "", true, nil, StatusWindowOpen},
		{"after one month court referral needs no note", "2026-04-16", "", true, nil, StatusCourtReferral},
		{"on the 12-month date needs no note", "2027-03-15", "", true, nil, StatusCourtReferral},
		{"after 12 months never expired", "2027-03-16", "", true, ErrSpecialCircumstances, StatusSpecialCircumstances},
		{"after 12 months with a note", "2027-03-16", "client was abroad", true, nil, StatusSpecialCircumstances},
		{"flag off", "2026-03-20", "", false, ErrRequisitionUnverified, StatusWindowOpen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, loc, ctx := requisitionFixture(t, tt.today, tt.flagOn)
			b := AssessedBill{ID: "0141", DeliveredAt: time.Date(2026, 3, 15, 15, 0, 0, 0, loc)}
			w, err := r.Windows(ctx, b)
			if err != nil {
				t.Fatalf("Windows: %v", err)
			}
			if got := w.ClientRequisitionTo.Format(time.DateOnly); got != "2026-04-15" {
				t.Errorf("ClientRequisitionTo = %s, want 2026-04-15", got)
			}
			if got := w.TwelveMonthTo.Format(time.DateOnly); got != "2027-03-15" {
				t.Errorf("TwelveMonthTo = %s, want 2027-03-15", got)
			}
			if w.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", w.Status, tt.wantStatus)
			}
			_, err = r.Record(ctx, b, tt.note)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Record = %v, want %v", err, tt.wantErr)
			}
			held := tt.wantErr == nil
			if r.RemindersAllowed(b.ID) == held || r.InterestAllowed(b.ID) == held {
				t.Errorf("reminders %v, interest %v; want both %v", r.RemindersAllowed(b.ID), r.InterestAllowed(b.ID), !held)
			}
			if r.OnHold("other") {
				t.Error("another bill is on hold")
			}
		})
	}
	r, _, ctx := requisitionFixture(t, "2026-04-01", true)
	if _, err := r.Record(ctx, AssessedBill{ID: "draft"}, ""); !errors.Is(err, ErrNotDelivered) {
		t.Errorf("Record undelivered = %v, want %v", err, ErrNotDelivered)
	}
}

// rule: BIL-L-006
func TestBIL_L_006_PaidBillNeedsSpecialCircumstancesWithinTwelveMonths(t *testing.T) {
	// Bill 0141 delivered 2026-04-20 for 120000 cents. Solicitors Act s.11: once paid, a
	// referral needs special circumstances and is barred 12 months after payment.
	tests := []struct {
		name       string
		today      string
		paidCents  int64
		paidOn     string // empty when not paid
		note       string
		wantErr    error
		wantStatus string
		wantHeld   bool
	}{
		{"unpaid within one month", "2026-05-01", 0, "", "", nil, StatusWindowOpen, true},
		{"part payment is not payment", "2026-05-01", 60000, "2026-04-25", "", nil, StatusWindowOpen, true},
		{"paid in full needs a note", "2026-05-01", 120000, "2026-04-25", "", ErrSpecialCircumstances, StatusSpecialCircumstances, false},
		{"paid in full with a note", "2026-05-01", 120000, "2026-04-25", "fees far above estimate", nil, StatusSpecialCircumstances, true},
		{"overpaid counts as paid", "2026-05-01", 130000, "2026-04-25", "", ErrSpecialCircumstances, StatusSpecialCircumstances, false},
		{"twelve months after payment still open", "2027-04-25", 120000, "2026-04-25", "fees far above estimate", nil, StatusSpecialCircumstances, true},
		{"day after twelve months barred even with a note", "2027-04-26", 120000, "2026-04-25", "fees far above estimate", ErrPaidReferralBarred, StatusPaidBarred, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, loc, ctx := requisitionFixture(t, tt.today, true)
			b := AssessedBill{ID: "0141", DeliveredAt: time.Date(2026, 4, 20, 10, 0, 0, 0, loc), TotalCents: 120000, PaidCents: tt.paidCents}
			if tt.paidOn != "" {
				paid, err := time.ParseInLocation(time.DateOnly, tt.paidOn, loc)
				if err != nil {
					t.Fatal(err)
				}
				b.PaidAt = paid.Add(14 * time.Hour)
			}
			w, err := r.Windows(ctx, b)
			if err != nil {
				t.Fatalf("Windows: %v", err)
			}
			if w.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", w.Status, tt.wantStatus)
			}
			if b.Paid() {
				if got := w.PaidReferralTo.Format(time.DateOnly); got != "2027-04-25" {
					t.Errorf("PaidReferralTo = %s, want 2027-04-25", got)
				}
			} else if !w.PaidReferralTo.IsZero() {
				t.Errorf("PaidReferralTo = %s on an unpaid bill", w.PaidReferralTo)
			}
			_, err = r.Record(ctx, b, tt.note)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Record = %v, want %v", err, tt.wantErr)
			}
			if r.OnHold(b.ID) != tt.wantHeld {
				t.Errorf("OnHold = %v, want %v", r.OnHold(b.ID), tt.wantHeld)
			}
		})
	}
}
