package interest

import (
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/money"
)

type flagStub bool

func (f flagStub) Enabled(name string) bool { return bool(f) && name == FlagSolicitorsActVerified }

func startFixture(t *testing.T) (Start, func(y int, mo time.Month, d int) time.Time) {
	t.Helper()
	loc := toronto(t)
	return Start{Calendar: Months{Loc: loc}, Flags: flagStub(true)},
		func(y int, mo time.Month, d int) time.Time { return time.Date(y, mo, d, 0, 0, 0, 0, loc) }
}

// rule: BIL-I-023
// rule: BIL-L-008 (verify one-month computation)
func TestBIL_I_023_NoInterestBeforeOneMonth(t *testing.T) {
	s, day := startFixture(t)
	b := Bill{ID: "b1", DeliveredAt: day(2026, 1, 15).Add(10 * time.Hour), Principal: 100000, RatePPM: 60000}
	for d := day(2026, 1, 15); d.Before(day(2026, 3, 1)); d = d.AddDate(0, 0, 1) {
		want := !d.Before(day(2026, 2, 16))
		if got := s.Accrues(b, time.Time{}, d); got != want {
			t.Fatalf("Accrues(%s) = %v, want %v", d.Format(time.DateOnly), got, want)
		}
	}
	tests := []struct {
		name string
		b    Bill
	}{
		{"no rate", Bill{ID: "b2", DeliveredAt: b.DeliveredAt}},
		{"not delivered", Bill{ID: "b3", RatePPM: 60000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if s.Accrues(tt.b, time.Time{}, day(2027, 1, 1)) {
				t.Fatal("interest accrues on a bill that must never accrue")
			}
		})
	}
}

// rule: BIL-L-008 (verify one-month computation)
func TestBIL_L_008_AccrualFromOneMonthAfterDelivery(t *testing.T) {
	s, day := startFixture(t)
	tests := []struct {
		name      string
		delivered time.Time
		want      time.Time
	}{
		{"mid month", day(2026, 1, 15).Add(9 * time.Hour), day(2026, 2, 16)},
		{"month end clamps", day(2026, 1, 31).Add(9 * time.Hour), day(2026, 3, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.From(Bill{ID: "b", DeliveredAt: tt.delivered, RatePPM: 60000}, time.Time{})
			if err != nil || !got.Equal(tt.want) {
				t.Fatalf("From = %s, %v; want %s", got.Format(time.DateOnly), err, tt.want.Format(time.DateOnly))
			}
		})
	}
}

// rule: BIL-C-070
func TestBIL_C_070_StartIsLaterOfDueAndOneMonth(t *testing.T) {
	s, day := startFixture(t)
	b := Bill{ID: "b", DeliveredAt: day(2026, 1, 15).Add(9 * time.Hour), RatePPM: 60000}
	tests := []struct {
		name string
		due  time.Time
		want time.Time
	}{
		{"no due date", time.Time{}, day(2026, 2, 16)},
		{"due before one month", day(2026, 1, 30), day(2026, 2, 16)},
		{"due after one month", day(2026, 3, 15), day(2026, 3, 15)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.From(b, tt.due)
			if err != nil || !got.Equal(tt.want) {
				t.Fatalf("From = %s, %v; want %s", got.Format(time.DateOnly), err, tt.want.Format(time.DateOnly))
			}
		})
	}
}

// rule: BIL-C-071
// rule: BIL-L-009
func TestBIL_C_071_RateMustBeAgreed(t *testing.T) {
	s, day := startFixture(t)
	tests := []struct {
		name string
		rate money.PPM
		want error
	}{
		{"no agreed rate", 0, ErrNoAgreedRate},
		{"agreed rate", 60000, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Bill{ID: "b", DeliveredAt: day(2026, 1, 15), RatePPM: tt.rate}
			_, err := s.From(b, time.Time{})
			if !errors.Is(err, tt.want) {
				t.Fatalf("From err = %v, want %v", err, tt.want)
			}
		})
	}
}

// rule: BIL-L-009
func TestBIL_L_009_NoPrintedRateNoInterest(t *testing.T) {
	s, day := startFixture(t)
	b := Bill{ID: "b", DeliveredAt: day(2026, 1, 15), Principal: 500000}
	tests := []struct {
		name string
		day  time.Time
	}{
		{"90 days overdue", day(2026, 5, 16)},
		{"one year later", day(2027, 1, 15)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if s.Accrues(b, time.Time{}, tt.day) {
				t.Fatal("interest accrues without a rate shown on the bill")
			}
		})
	}
}

// rule: BIL-L-012
func TestBIL_L_012_NoInterestBeforeDelivery(t *testing.T) {
	s, day := startFixture(t)
	tests := []struct {
		name      string
		delivered time.Time
		day       time.Time
		want      bool
	}{
		{"security given, bill not delivered", time.Time{}, day(2026, 3, 1), false},
		{"delivered 03-01, 03-31 blocked", day(2026, 3, 1), day(2026, 3, 31), false},
		{"delivered 03-01, 04-01 blocked", day(2026, 3, 1), day(2026, 4, 1), false},
		{"delivered 03-01, 04-02 accrues", day(2026, 3, 1), day(2026, 4, 2), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Bill{ID: "b", DeliveredAt: tt.delivered, RatePPM: 60000}
			if got := s.Accrues(b, time.Time{}, tt.day); got != tt.want {
				t.Fatalf("Accrues = %v, want %v", got, tt.want)
			}
		})
	}
}

// rule: BIL-C-072
func TestBIL_C_072_PostingClippedToStart(t *testing.T) {
	s, day := startFixture(t)
	b := Bill{ID: "b", DeliveredAt: day(2026, 1, 15), RatePPM: 60000}
	tests := []struct {
		name    string
		p       Period
		want    Period
		wantErr error
	}{
		{"month end posting starts at accrual start", Period{day(2026, 2, 1), day(2026, 3, 1)}, Period{day(2026, 2, 16), day(2026, 3, 1)}, nil},
		{"later period unchanged", Period{day(2026, 3, 1), day(2026, 4, 1)}, Period{day(2026, 3, 1), day(2026, 4, 1)}, nil},
		{"period ends on start", Period{day(2026, 2, 1), day(2026, 2, 16)}, Period{}, ErrNotDue},
		{"reversed period", Period{day(2026, 3, 1), day(2026, 2, 1)}, Period{}, ErrBadPeriod},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Clip(b, time.Time{}, tt.p)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Clip err = %v, want %v", err, tt.wantErr)
			}
			if !got.From.Equal(tt.want.From) || !got.To.Equal(tt.want.To) {
				t.Fatalf("Clip = %v, want %v", got, tt.want)
			}
		})
	}
}

// rule: BIL-C-073
func TestBIL_C_073_TorontoDeliveryDate(t *testing.T) {
	s, day := startFixture(t)
	tests := []struct {
		name      string
		delivered time.Time
		want      time.Time
	}{
		{"03:30Z is 23:30 the day before in Toronto", time.Date(2026, 11, 1, 3, 30, 0, 0, time.UTC), day(2026, 12, 1)},
		{"same instant late in UTC day", time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC), day(2026, 12, 2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.From(Bill{ID: "b", DeliveredAt: tt.delivered, RatePPM: 60000}, time.Time{})
			if err != nil || !got.Equal(tt.want) {
				t.Fatalf("From = %s, %v; want %s", got.Format(time.DateOnly), err, tt.want.Format(time.DateOnly))
			}
		})
	}
}

func TestStartFlagOff(t *testing.T) {
	s, day := startFixture(t)
	s.Flags = flagStub(false)
	_, err := s.From(Bill{ID: "b", DeliveredAt: day(2026, 1, 15), RatePPM: 60000}, time.Time{})
	if !errors.Is(err, ErrAccrualUnverified) {
		t.Fatalf("From err = %v, want ErrAccrualUnverified", err)
	}
}
