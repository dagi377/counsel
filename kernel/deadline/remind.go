package deadline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

// Notice kinds the reminder worker sends.
const (
	NoticeReminder = "deadline.reminder"
	NoticeDigest   = "deadline.digest"
)

// DefaultReminderWindow is how many days ahead a deadline is reminded, every day until due.
const DefaultReminderWindow = 7

// Reminder is one deadline due soon. It carries ids and dates only, never client content.
type Reminder struct {
	DeadlineID DeadlineID
	Subject    Subject
	State      State
	Due        Date
	// DaysLeft counts calendar days from today to Due; BusinessDaysLeft counts weekdays
	// that are not holidays.
	DaysLeft         int
	BusinessDaysLeft int
}

// Notice is what a Notifier delivers: one reminder, or the day's digest of every reminder.
type Notice struct {
	Kind  string
	Day   Date
	Items []Reminder
}

// Notifier delivers a notice. The reminder worker never depends on the agent: a notifier is
// a plain delivery channel.
type Notifier interface {
	Notify(ctx context.Context, n Notice) error
}

// SentKey names one sent notice: its kind, its deadline (empty for a digest), its recipient
// (empty for the firm channel) and the business day it was sent for.
type SentKey struct {
	Kind       string
	DeadlineID DeadlineID
	UserID     string
	Day        Date
}

func (k SentKey) String() string {
	return fmt.Sprintf("%s:%s:%s:%04d-%02d-%02d", k.Kind, k.DeadlineID, k.UserID, k.Day.Year, int(k.Day.Month), k.Day.Day)
}

// SentLog remembers which notices went out, so a second run the same day sends nothing.
// PGSentLog keeps it across restarts.
type SentLog interface {
	// Sent reports whether key was marked.
	Sent(ctx context.Context, key SentKey) (bool, error)
	// Mark records key as sent. Marking a key twice is not an error.
	Mark(ctx context.Context, key SentKey) error
}

// MemSentLog is an in-memory SentLog for tests and a process without a database.
type MemSentLog struct {
	mu   sync.Mutex
	keys map[SentKey]bool
}

// Sent implements SentLog.
func (m *MemSentLog) Sent(_ context.Context, key SentKey) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.keys[key], nil
}

// Mark implements SentLog.
func (m *MemSentLog) Mark(_ context.Context, key SentKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys == nil {
		m.keys = map[SentKey]bool{}
	}
	m.keys[key] = true
	return nil
}

// ErrReminderDependency is returned when the worker lacks a dependency it needs.
var ErrReminderDependency = errors.New("deadline: reminder dependency missing")

// Reminders is the deadline reminder worker (T-031.4). Each run lists the deadlines due from
// today to today+Window and sends each one reminder per day, through Primary or, when Primary
// fails, through Fallback (ARC-U-030). It reads only the deadline store and the clock, so it
// sends while the agent is down; run hourly, no reminder is more than an hour late (SLO-04).
type Reminders struct {
	Deadlines Lister
	Clock     clock.Clock
	Primary   Notifier
	Fallback  Notifier
	Sent      SentLog
	// Window is the look-ahead in days; zero means DefaultReminderWindow.
	Window int
	// Holidays returns the holiday calendar for BusinessDaysLeft; nil counts weekends only.
	Holidays func(ctx context.Context) ([]Date, error)
}

// RunResult says what one run did.
type RunResult struct {
	Sent     int // notices delivered
	Fallback int // of which through the fallback notifier
	Skipped  int // already sent today
}

func (r *Reminders) check() error {
	switch {
	case r.Deadlines == nil:
		return fmt.Errorf("reminders: Deadlines: %w", ErrReminderDependency)
	case r.Clock == nil:
		return fmt.Errorf("reminders: Clock: %w", ErrReminderDependency)
	case r.Primary == nil:
		return fmt.Errorf("reminders: Primary: %w", ErrReminderDependency)
	case r.Sent == nil:
		return fmt.Errorf("reminders: Sent: %w", ErrReminderDependency)
	}
	return nil
}

// due lists today's reminders, by due date.
func (r *Reminders) due(ctx context.Context) (Date, []Reminder, error) {
	if err := r.check(); err != nil {
		return Date{}, nil, err
	}
	today := r.Clock.Today(ctx)
	window := r.Window
	if window <= 0 {
		window = DefaultReminderWindow
	}
	to := civilDate(civilTime(today).AddDate(0, 0, window))
	ds, err := r.Deadlines.List(ctx, Filter{From: &today, To: &to})
	if err != nil {
		return Date{}, nil, fmt.Errorf("reminders: list: %w", err)
	}
	hol := map[Date]bool{}
	if r.Holidays != nil {
		hs, err := r.Holidays(ctx)
		if err != nil {
			return Date{}, nil, fmt.Errorf("reminders: holidays: %w", err)
		}
		for _, h := range hs {
			hol[h] = true
		}
	}
	out := make([]Reminder, 0, len(ds))
	for _, d := range ds {
		// Only open deadlines are reminded: met, released and superseded ones are done.
		if d.State != StateProposed && d.State != StateConfirmed {
			continue
		}
		out = append(out, Reminder{DeadlineID: d.ID, Subject: d.Proposed.Subject, State: d.State, Due: d.Proposed.Due,
			DaysLeft: daysBetween(today, d.Proposed.Due), BusinessDaysLeft: businessDaysBetween(today, d.Proposed.Due, hol)})
	}
	return today, out, nil
}

// Remind sends each deadline due in the window its reminder for today, once.
func (r *Reminders) Remind(ctx context.Context) (RunResult, error) {
	today, rs, err := r.due(ctx)
	if err != nil {
		return RunResult{}, err
	}
	var res RunResult
	var errs []error
	for _, rm := range rs {
		key := SentKey{Kind: NoticeReminder, DeadlineID: rm.DeadlineID, Day: today}
		if err := r.send(ctx, key, Notice{Kind: NoticeReminder, Day: today, Items: []Reminder{rm}}, &res); err != nil {
			errs = append(errs, err)
		}
	}
	return res, errors.Join(errs...)
}

// Digest sends today's digest of every deadline in the window, once. A day with nothing due
// sends nothing.
func (r *Reminders) Digest(ctx context.Context) (RunResult, error) {
	today, rs, err := r.due(ctx)
	if err != nil || len(rs) == 0 {
		return RunResult{}, err
	}
	var res RunResult
	key := SentKey{Kind: NoticeDigest, Day: today}
	return res, r.send(ctx, key, Notice{Kind: NoticeDigest, Day: today, Items: rs}, &res)
}

// send delivers n once per key: Primary first, then Fallback.
func (r *Reminders) send(ctx context.Context, key SentKey, n Notice, res *RunResult) error {
	done, err := r.Sent.Sent(ctx, key)
	if err != nil {
		return fmt.Errorf("reminders: sent log: %w", err)
	}
	if done {
		res.Skipped++
		return nil
	}
	// rule: ARC-U-030
	perr := r.Primary.Notify(ctx, n)
	if perr != nil {
		if r.Fallback == nil {
			return fmt.Errorf("reminders: %s: primary: %w", key, perr)
		}
		if ferr := r.Fallback.Notify(ctx, n); ferr != nil {
			return fmt.Errorf("reminders: %s: primary: %w; fallback: %w", key, perr, ferr)
		}
		res.Fallback++
	}
	res.Sent++
	if err := r.Sent.Mark(ctx, key); err != nil {
		return fmt.Errorf("reminders: mark %s: %w", key, err)
	}
	return nil
}

func civilTime(d Date) time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

func civilDate(t time.Time) Date { return Date{Year: t.Year(), Month: t.Month(), Day: t.Day()} }

func daysBetween(from, to Date) int { return int(civilTime(to).Sub(civilTime(from)).Hours() / 24) }

// businessDaysBetween counts the weekdays after from up to and including to that are not
// holidays.
func businessDaysBetween(from, to Date, hol map[Date]bool) int {
	n := 0
	for t := civilTime(from).AddDate(0, 0, 1); !t.After(civilTime(to)); t = t.AddDate(0, 0, 1) {
		if wd := t.Weekday(); wd != time.Saturday && wd != time.Sunday && !hol[civilDate(t)] {
			n++
		}
	}
	return n
}
