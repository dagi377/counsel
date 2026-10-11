package deadline_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
)

type recordNotifier struct {
	fail    bool
	notices []deadline.Notice
}

func (n *recordNotifier) Notify(_ context.Context, x deadline.Notice) error {
	if n.fail {
		return errors.New("channel down")
	}
	n.notices = append(n.notices, x)
	return nil
}

func toronto(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// seedDue stores a deadline due on due.
func seedDue(t *testing.T, f *deadlinefake.Fake, due deadline.Date) {
	t.Helper()
	if _, err := f.Save(context.Background(), nil, deadline.Proposed{PackID: "p", PackVersion: "1", RuleID: "r",
		Due: due, Subject: deadline.Subject{Kind: "case", ID: "c1"}}); err != nil {
		t.Fatal(err)
	}
}

func TestRemindersOncePerDay(t *testing.T) {
	// rule: ARC-U-030
	loc := toronto(t)
	tests := []struct {
		name           string
		primaryDown    bool
		noFallback     bool
		dueIn          int
		wantSent       int
		wantFallback   int
		wantErr        bool
		wantBusinessDs int
	}{
		{name: "due in 2 days, primary up", dueIn: 2, wantSent: 1, wantBusinessDs: 2},
		{name: "primary down falls back", primaryDown: true, dueIn: 2, wantSent: 1, wantFallback: 1, wantBusinessDs: 2},
		{name: "both down is an error and retries", primaryDown: true, noFallback: true, dueIn: 2, wantErr: true},
		{name: "outside the window sends nothing", dueIn: 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			clk := clockfake.New(time.Date(2026, 10, 13, 9, 0, 0, 0, loc), loc) // a Tuesday
			store := deadlinefake.New()
			seedDue(t, store, clk.Today(ctx).AddDays(tt.dueIn))
			primary, fallback := &recordNotifier{fail: tt.primaryDown}, &recordNotifier{}
			r := &deadline.Reminders{Deadlines: store, Clock: clk, Primary: primary, Fallback: fallback, Sent: &deadline.MemSentLog{}}
			if tt.noFallback {
				r.Fallback = nil
			}
			res, err := r.Remind(ctx)
			if (err != nil) != tt.wantErr || res.Sent != tt.wantSent || res.Fallback != tt.wantFallback {
				t.Fatalf("first run = %+v, %v", res, err)
			}
			if tt.wantSent == 1 {
				got := append(primary.notices, fallback.notices...)
				if len(got) != 1 || got[0].Items[0].DaysLeft != tt.dueIn || got[0].Items[0].BusinessDaysLeft != tt.wantBusinessDs {
					t.Fatalf("notices = %+v", got)
				}
			}
			// A second run the same day sends nothing new.
			clk.Advance(5 * time.Hour)
			again, _ := r.Remind(ctx)
			if again.Sent != 0 && !tt.wantErr {
				t.Fatalf("second run same day sent %d", again.Sent)
			}
			// The next day reminds again while still in the window.
			clk.Advance(24 * time.Hour)
			if tt.wantSent == 1 {
				next, err := r.Remind(ctx)
				if err != nil || next.Sent != 1 {
					t.Fatalf("next day = %+v, %v", next, err)
				}
			}
		})
	}
}

func TestRemindersDigestAndHolidays(t *testing.T) {
	loc := toronto(t)
	ctx := context.Background()
	clk := clockfake.New(time.Date(2026, 10, 9, 7, 0, 0, 0, loc), loc) // Friday before Thanksgiving
	store := deadlinefake.New()
	seedDue(t, store, deadline.Date{Year: 2026, Month: time.October, Day: 13})
	seedDue(t, store, deadline.Date{Year: 2026, Month: time.October, Day: 14})
	tests := []struct {
		name     string
		holidays []deadline.Date
		wantBD   int // business days to Oct 13
	}{
		{"weekends only", nil, 2},
		{"with Thanksgiving", []deadline.Date{{Year: 2026, Month: time.October, Day: 12}}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &recordNotifier{}
			r := &deadline.Reminders{Deadlines: store, Clock: clk, Primary: n, Sent: &deadline.MemSentLog{},
				Holidays: func(context.Context) ([]deadline.Date, error) { return tt.holidays, nil }}
			res, err := r.Digest(ctx)
			if err != nil || res.Sent != 1 || len(n.notices) != 1 || n.notices[0].Kind != deadline.NoticeDigest || len(n.notices[0].Items) != 2 {
				t.Fatalf("digest = %+v, %v, %+v", res, err, n.notices)
			}
			if bd := n.notices[0].Items[0].BusinessDaysLeft; bd != tt.wantBD {
				t.Fatalf("business days = %d, want %d", bd, tt.wantBD)
			}
			if res, _ := r.Digest(ctx); res.Sent != 0 || res.Skipped != 1 {
				t.Fatalf("second digest = %+v", res)
			}
		})
	}
}

func TestRemindersMissingDependency(t *testing.T) {
	if _, err := (&deadline.Reminders{}).Remind(context.Background()); !errors.Is(err, deadline.ErrReminderDependency) {
		t.Fatalf("err = %v", err)
	}
}
