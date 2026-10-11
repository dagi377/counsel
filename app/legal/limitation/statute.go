package limitation

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/deadline"
)

var _ Calculator = (*Statute)(nil)

// Statute is the Calculator for the Limitations Act, 2002, S.O. 2002, c. 24, Sch. B. Every
// rule below is unverified law: Compute fails closed with ErrUnverified unless the
// deadline.ontario_pack flag is on in the request context.
//
// Dates are calendar dates in America/Toronto. A time at midnight in its own location is read
// as the date it names; any other instant is converted to its America/Toronto date. Each
// Period.Due is midnight America/Toronto of the last day on which the proceeding may be
// commenced. When both a basic and an ultimate period are returned, the claim is barred at
// the earlier of the two.
type Statute struct {
	schedule Schedule
	loc      *time.Location
	holidays []clock.Date // the rule pack's holidays (Legislation Act, 2006 s.88)
	roll     bool         // a holiday calendar is attached, so s.88 applies
}

// NewStatute returns the Ontario calculator. schedule may be nil when no Schedule periods
// are loaded.
func NewStatute(schedule Schedule) (*Statute, error) {
	loc, err := time.LoadLocation(clock.DefaultZone)
	if err != nil {
		return nil, fmt.Errorf("limitation: load %s: %w", clock.DefaultZone, err)
	}
	return &Statute{schedule: schedule, loc: loc}, nil
}

// WithHolidays returns s using holidays, the rule pack's holiday calendar, for the
// Legislation Act, 2006 s.88 roll: a period that ends on a holiday or a weekend runs to the
// next business day (Saturday, Sunday or a holiday in the calendar). Boot attaches the
// Ontario pack's calendar; a Statute without one returns calendar last days.
func (s *Statute) WithHolidays(holidays []clock.Date) *Statute {
	c := *s
	c.holidays, c.roll = slices.Clone(holidays), true
	return &c
}

// Period lengths in calendar years.
const (
	basicYears        = 2  // s.4
	ultimateYears     = 15 // s.15(2)
	contributionYears = 2  // s.18 with s.4
)

// Compute implements Calculator.
func (s *Statute) Compute(ctx context.Context, a Assessment) ([]Period, error) {
	// (verify) Limitations Act, 2002: unconfirmed law stays off; fail closed.
	if !legalflags.Enabled(ctx, legalflags.DeadlineOntarioPack) {
		return nil, fmt.Errorf("compute: %w", ErrUnverified)
	}
	if (a.MatterID == "") == (a.LeadID == "") {
		return nil, fmt.Errorf("compute: %w", ErrOwner)
	}
	switch a.NoLimit {
	case "", NoLimitEnvironmental:
	// (verify) Limitations Act, 2002 s.16(1)(h) and (h.1): no limitation period for a
	// proceeding based on a sexual assault or on other misconduct of a sexual nature.
	case NoLimitSexualAssault, NoLimitSexualMisconduct:
		return nil, fmt.Errorf("compute %s: %w", a.NoLimit, ErrNoPeriod)
	default:
		return nil, fmt.Errorf("compute: unknown no-limit kind %q: %w", a.NoLimit, ErrNoPeriod)
	}
	if a.ClaimType != "" && s.schedule != nil {
		sp, ok, err := s.schedule.Lookup(ctx, a.ClaimType)
		if err != nil {
			return nil, fmt.Errorf("compute: schedule lookup: %w", err)
		}
		if ok {
			return s.scheduled(a, sp)
		}
	}
	act, disc, discovered, err := s.facts(a)
	if err != nil {
		return nil, err
	}
	// (verify) Limitations Act, 2002 s.17: no limitation period for an environmental claim
	// that has not been discovered. Once discovered, only the basic period runs.
	if a.NoLimit == NoLimitEnvironmental && !discovered {
		return nil, fmt.Errorf("compute %s: %w", a.NoLimit, ErrNoPeriod)
	}
	if !discovered {
		return nil, fmt.Errorf("compute: discovery date or s.5(2) presumption: %w", ErrDates)
	}
	susp, err := s.intervals(a.Suspensions)
	if err != nil {
		return nil, err
	}

	var out []Period
	years := basicYears
	if a.ContributionServedDate != nil {
		years = contributionYears
	}
	// (verify) Limitations Act, 2002 s.4: 2 years from the day the claim was discovered.
	// s.6 and s.7: the basic period does not run while the claimant is a minor or incapable
	// and not represented by a litigation guardian. s.22: an agreement may suspend it.
	// Wilful concealment goes to discovery (s.5), so it does not suspend the basic period.
	basic, open := run(disc, years, filter(susp, SuspendMinority, SuspendIncapacity, SuspendAgreement))
	if !open {
		out = append(out, Period{Kind: "basic", Due: s.at(basic)})
	}
	if a.NoLimit != NoLimitEnvironmental {
		// (verify) Limitations Act, 2002 s.15(2): 15 years from the day of the act or
		// omission. s.15(4): it does not run while the claimant is incapable or a minor and
		// not represented by a litigation guardian, or while the defendant wilfully conceals
		// the injury or misleads the claimant. s.22: an agreement may suspend it.
		ult, open := run(act, ultimateYears, filter(susp, SuspendMinority, SuspendIncapacity, SuspendConcealment, SuspendAgreement))
		if !open {
			out = append(out, Period{Kind: "ultimate", Due: s.at(ult)})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("compute: %w", ErrSuspended)
	}
	return out, nil
}

// facts resolves the day of the act or omission and the day of discovery. discovered is
// false when no discovery date is proved and the s.5(2) presumption is not relied on.
func (s *Statute) facts(a Assessment) (act, disc time.Time, discovered bool, err error) {
	switch {
	// (verify) Limitations Act, 2002 s.18(1): for contribution and indemnity, the day the
	// claimant wrongdoer was served with the claim is deemed the day of the act or omission,
	// for s.5(2) and s.15.
	case a.ContributionServedDate != nil:
		act = s.day(*a.ContributionServedDate)
	case a.ActDate != nil:
		act = s.day(*a.ActDate)
	default:
		return act, disc, false, fmt.Errorf("compute: act or omission date: %w", ErrDates)
	}
	// (verify) Limitations Act, 2002 s.5(2): the claimant is presumed to have discovered the
	// claim on the day of the act or omission unless the contrary is proved.
	if a.DiscoveryPresumed {
		if a.DiscoveryDate != nil && !s.day(*a.DiscoveryDate).Equal(act) {
			return act, disc, false, fmt.Errorf("compute: presumed discovery differs from act date: %w", ErrDates)
		}
		return act, act, true, nil
	}
	if a.DiscoveryDate == nil {
		return act, disc, false, nil
	}
	disc = s.day(*a.DiscoveryDate)
	if disc.Before(act) {
		return act, disc, false, fmt.Errorf("compute: discovery before act: %w", ErrDates)
	}
	return act, disc, true, nil
}

// scheduled computes a period set by another Act listed in the Schedule.
func (s *Statute) scheduled(a Assessment, sp SchedulePeriod) ([]Period, error) {
	if sp.Years < 0 || sp.Months < 0 || sp.Days < 0 || sp.Years+sp.Months+sp.Days == 0 {
		return nil, fmt.Errorf("compute: schedule period %q is empty or negative: %w", sp.Provision, ErrNoPeriod)
	}
	act, disc, discovered, err := s.facts(a)
	if err != nil {
		return nil, err
	}
	start := act
	if sp.FromDiscovery {
		if !discovered {
			return nil, fmt.Errorf("compute: schedule period runs from discovery: %w", ErrDates)
		}
		start = disc
	}
	// (verify) Limitations Act, 2002 s.19: a period in another Act applies only if the
	// provision is listed in the Schedule; it then replaces the basic and ultimate periods.
	// Suspensions are not applied to it.
	due := addDays(addMonths(start, sp.Years*12+sp.Months), sp.Days)
	return []Period{{Kind: "schedule", Due: s.at(due)}}, nil
}

// interval is a suspension as calendar days [from, to); open has no end.
type interval struct {
	kind     SuspensionKind
	from, to time.Time
	open     bool
}

func (s *Statute) intervals(in []Suspension) ([]interval, error) {
	out := make([]interval, 0, len(in))
	for _, x := range in {
		iv := interval{kind: x.Kind, from: s.day(x.From), open: x.To == nil}
		if x.To != nil {
			iv.to = s.day(*x.To)
			if iv.to.Before(iv.from) {
				return nil, fmt.Errorf("compute: suspension %s ends before it starts: %w", x.Kind, ErrDates)
			}
		}
		out = append(out, iv)
	}
	return out, nil
}

func filter(in []interval, kinds ...SuspensionKind) []interval {
	var out []interval
	for _, iv := range in {
		if slices.Contains(kinds, iv.kind) {
			out = append(out, iv)
		}
	}
	return out
}

// run returns the last day of a period of years that starts on start and does
// not run during susp. A suspension covering the start day defers the start to its end, so
// the period is counted in calendar years from that day. A suspension inside the running
// period extends the due date by its length in days; the extension is repeated until no
// further suspension falls inside it. open is true when an open-ended suspension stops the
// period before it ends.
func run(start time.Time, years int, susp []interval) (due time.Time, open bool) {
	for moved := true; moved; {
		moved = false
		for _, iv := range susp {
			if iv.from.After(start) || (!iv.open && !iv.to.After(start)) {
				continue
			}
			if iv.open {
				return time.Time{}, true
			}
			start, moved = iv.to, true
		}
	}
	nominal := addMonths(start, years*12)
	due = nominal
	for {
		n, stopped := suspendedDays(start, due, susp)
		if stopped {
			return time.Time{}, true
		}
		next := addDays(nominal, n)
		if next.Equal(due) {
			return due, false
		}
		due = next
	}
}

// suspendedDays counts the days in [start, end) covered by any suspension. stopped is true
// when an open-ended suspension begins in that range.
func suspendedDays(start, end time.Time, susp []interval) (n int, stopped bool) {
	for d := start; d.Before(end); d = addDays(d, 1) {
		for _, iv := range susp {
			if iv.from.After(d) {
				continue
			}
			if iv.open {
				return 0, true
			}
			if iv.to.After(d) {
				n++
				break
			}
		}
	}
	return n, false
}

// day reads t as an America/Toronto calendar date and returns it as UTC midnight, the form
// all arithmetic here uses.
func (s *Statute) day(t time.Time) time.Time {
	if h, m, sec := t.Clock(); h == 0 && m == 0 && sec == 0 && t.Nanosecond() == 0 {
		y, mo, d := t.Date()
		return time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
	}
	y, mo, d := t.In(s.loc).Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
}

// at returns the last day of a period ending on calendar date d, as midnight
// America/Toronto. (verify) Legislation Act, 2006 s.88: when the last day falls on a holiday
// (Saturday and Sunday included), the period ends on the next day that is not a holiday.
func (s *Statute) at(d time.Time) time.Time {
	y, mo, dd := d.Date()
	day := clock.Date{Year: y, Month: mo, Day: dd}
	// rule: Legislation Act, 2006 s.88 (verify)
	for s.roll && !deadline.IsBusinessDay(day, s.holidays) {
		day = day.AddDays(1)
	}
	return day.Start(s.loc)
}

// addMonths adds n calendar months to a UTC-midnight date. (verify) Legislation Act, 2006
// s.89: when the anniversary day does not exist in the final month (Feb 29 in a common year),
// the period ends on the last day of that month, so Feb 29 + 2 years is Feb 28.
func addMonths(d time.Time, n int) time.Time {
	y, m, day := d.Date()
	first := time.Date(y, m+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	if day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, time.UTC)
}

func addDays(d time.Time, n int) time.Time { return d.AddDate(0, 0, n) }
