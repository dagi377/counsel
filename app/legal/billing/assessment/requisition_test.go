package assessment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/interest/interestfake"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/flags/flagsfake"
)

func requisitionFixture(t *testing.T, today string, flagOn bool) (*Requisitions, *time.Location) {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	now, err := time.ParseInLocation(time.DateOnly, today, loc)
	if err != nil {
		t.Fatal(err)
	}
	f := flagsfake.New()
	if err := f.Define(flags.Def{Name: requisitionFlag, Owner: "lawyer/LSO"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(requisitionFlag, flagOn); err != nil {
		t.Fatal(err)
	}
	return &Requisitions{Calendar: interestfake.New(loc), Clock: clockfake.New(now.Add(12*time.Hour), loc), Flags: f}, loc
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
			r, loc := requisitionFixture(t, tt.today, tt.flagOn)
			b := AssessedBill{ID: "0141", DeliveredAt: time.Date(2026, 3, 15, 15, 0, 0, 0, loc)}
			w, err := r.Windows(context.Background(), b)
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
			_, err = r.Record(context.Background(), b, tt.note)
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
	r, _ := requisitionFixture(t, "2026-04-01", true)
	if _, err := r.Record(context.Background(), AssessedBill{ID: "draft"}, ""); !errors.Is(err, ErrNotDelivered) {
		t.Errorf("Record undelivered = %v, want %v", err, ErrNotDelivered)
	}
}

// rule: BIL-L-006
func TestBIL_L_006_PaidBillStillAssessable(t *testing.T) {
	tests := []struct {
		name string
		paid bool
	}{
		{"unpaid", false},
		{"paid in full", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, loc := requisitionFixture(t, "2026-05-01", true)
			b := AssessedBill{ID: "0141", DeliveredAt: time.Date(2026, 4, 20, 10, 0, 0, 0, loc), Paid: tt.paid}
			if _, err := r.Record(context.Background(), b, ""); err != nil {
				t.Fatalf("Record: %v", err)
			}
			if !r.OnHold(b.ID) {
				t.Error("requisition not recorded")
			}
		})
	}
}
