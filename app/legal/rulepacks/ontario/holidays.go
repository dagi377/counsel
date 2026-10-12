package ontario

import (
	_ "embed"
	"fmt"
	"slices"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/kernel/clock"
)

//go:embed holidays.yaml
var holidaysYAML []byte

// WindowYears is how many years ahead of the current year the generated calendars cover.
// The previous year is covered too, so a date entered late still rolls.
const WindowYears = 10

// Calendar is a holiday definition encoded as rules, so it generates the dated holidays of
// any year. Every rule is unverified law: callers use a calendar only behind the
// deadline.ontario_pack flag. Sundays (and, for the court calendar, Saturdays) are holidays
// by definition but are not listed; day counting skips weekends itself.
type Calendar struct {
	name string
	// weekendSubs gives the substitute days for a named holiday that falls on a weekend.
	weekendSubs func(named holiday) []clock.Date
}

// holiday is one named holiday in one year.
type holiday struct {
	name string
	date clock.Date
}

// CourtCalendar is the Rules of Civil Procedure, R.R.O. 1990, Reg. 194, r. 1.03(1)
// "holiday" calendar (verify). Court and procedural deadlines (r. 3.01) use it.
//
// r. 1.03(1) "holiday": any Saturday or Sunday, the named holidays, and:
//   - New Year's Day, Canada Day or Remembrance Day on a Saturday or Sunday: the following Monday;
//   - Christmas Day on a Saturday or Sunday: the following Monday and Tuesday;
//   - Christmas Day on a Friday: the following Monday (Boxing Day falls on the Saturday).
func CourtCalendar() Calendar {
	return Calendar{name: "ontario_court", weekendSubs: func(h holiday) []clock.Date {
		wd := weekday(h.date)
		switch h.name {
		// rule: Rules of Civil Procedure r. 1.03(1) "holiday" (verify)
		case newYearsDay, canadaDay, remembranceDay:
			if wd == time.Saturday || wd == time.Sunday {
				return []clock.Date{nextMonday(h.date)}
			}
		case christmasDay:
			switch wd {
			case time.Saturday, time.Sunday:
				m := nextMonday(h.date)
				return []clock.Date{m, m.AddDays(1)}
			case time.Friday:
				return []clock.Date{nextMonday(h.date)}
			}
		}
		return nil
	}}
}

// LegislationActCalendar is the Legislation Act, 2006, S.O. 2006, c. 21, Sch. F, s. 87
// "holiday" calendar (verify). Limitation periods use it with the s. 88 roll forward.
//
// s. 87 "holiday": Sunday, the named holidays, and only these Sunday substitutes (no
// Saturday substitutes; Saturday is not a holiday):
//   - New Year's Day, Canada Day or Remembrance Day on a Sunday: the following Monday;
//   - Christmas Day on a Sunday: the following Tuesday (Boxing Day is the Monday).
func LegislationActCalendar() Calendar {
	return Calendar{name: "ontario_legislation_act", weekendSubs: func(h holiday) []clock.Date {
		if weekday(h.date) != time.Sunday {
			return nil
		}
		switch h.name {
		// rule: Legislation Act, 2006 s.87 "holiday" (verify)
		case newYearsDay, canadaDay, remembranceDay:
			return []clock.Date{h.date.AddDays(1)}
		case christmasDay:
			return []clock.Date{h.date.AddDays(2)}
		}
		return nil
	}}
}

// Name names the calendar in events and errors.
func (c Calendar) Name() string { return c.name }

// Holidays returns the calendar's dated holidays for the years from to to, inclusive, in
// date order, each once. Named holidays on a Saturday are listed for the record; those on
// a Sunday are not, since every Sunday is a holiday under both definitions.
func (c Calendar) Holidays(from, to int) []clock.Date {
	var out []clock.Date
	for y := from; y <= to; y++ {
		for _, h := range namedHolidays(y) {
			if weekday(h.date) != time.Sunday {
				out = append(out, h.date)
			}
			out = append(out, c.weekendSubs(h)...)
		}
	}
	slices.SortFunc(out, func(a, b clock.Date) int {
		return a.Start(time.UTC).Compare(b.Start(time.UTC))
	})
	return slices.Compact(out)
}

// Window returns the years the generated calendars cover on day now: the previous year
// through WindowYears years ahead.
func Window(now clock.Date) (from, to int) { return now.Year - 1, now.Year + WindowYears }

// Holidays returns the court calendar (CourtCalendar) over the Window of today in
// America/Toronto. Procedural deadlines, reminders and shift_date use it. It is unverified
// law: callers use it only behind the deadline.ontario_pack flag.
func Holidays() ([]clock.Date, error) {
	from, to, err := window()
	if err != nil {
		return nil, err
	}
	return CourtCalendar().Holidays(from, to), nil
}

// LegislationActHolidays returns the Legislation Act calendar (LegislationActCalendar) over
// the Window of today in America/Toronto, with the years it covers. Limitation periods use
// it. It is unverified law: callers use it only behind the deadline.ontario_pack flag.
func LegislationActHolidays() (hs []clock.Date, from, to int, err error) {
	from, to, err = window()
	if err != nil {
		return nil, 0, 0, err
	}
	return LegislationActCalendar().Holidays(from, to), from, to, nil
}

// window checks holidays.yaml (the pack header) and returns today's Window.
func window() (from, to int, err error) {
	var f struct {
		TimeZone string `yaml:"time_zone"`
	}
	if err := yaml.Unmarshal(holidaysYAML, &f); err != nil {
		return 0, 0, fmt.Errorf("ontario holidays: %w", err)
	}
	if f.TimeZone != clock.DefaultZone {
		return 0, 0, fmt.Errorf("ontario holidays: time_zone %q, want %q", f.TimeZone, clock.DefaultZone)
	}
	loc, err := time.LoadLocation(clock.DefaultZone)
	if err != nil {
		return 0, 0, fmt.Errorf("ontario holidays: %w", err)
	}
	from, to = Window(clock.DateOf(time.Now(), loc))
	return from, to, nil
}

const (
	newYearsDay    = "new_years_day"
	familyDay      = "family_day"
	goodFriday     = "good_friday"
	easterMonday   = "easter_monday"
	victoriaDay    = "victoria_day"
	canadaDay      = "canada_day"
	civicHoliday   = "civic_holiday"
	labourDay      = "labour_day"
	thanksgiving   = "thanksgiving_day"
	remembranceDay = "remembrance_day"
	christmasDay   = "christmas_day"
	boxingDay      = "boxing_day"
)

// namedHolidays returns the 12 named holidays both definitions share for year y.
// (verify) Both r. 1.03(1) and Legislation Act s. 87 name: New Year's Day, Family Day
// (third Monday in February), Good Friday, Easter Monday, Victoria Day (the Monday before
// May 25), Canada Day, Civic Holiday (first Monday in August), Labour Day (first Monday in
// September), Thanksgiving Day (second Monday in October), Remembrance Day, Christmas Day
// and Boxing Day. Days proclaimed as holidays (for example a federal Sep 30) are not
// generated; adding one needs a lawyer's confirmation.
func namedHolidays(y int) []holiday {
	easter := easterSunday(y)
	return []holiday{
		{newYearsDay, clock.Date{Year: y, Month: time.January, Day: 1}},
		{familyDay, nthMonday(y, time.February, 3)},
		{goodFriday, easter.AddDays(-2)},
		{easterMonday, easter.AddDays(1)},
		{victoriaDay, mondayBefore(clock.Date{Year: y, Month: time.May, Day: 25})},
		{canadaDay, clock.Date{Year: y, Month: time.July, Day: 1}},
		{civicHoliday, nthMonday(y, time.August, 1)},
		{labourDay, nthMonday(y, time.September, 1)},
		{thanksgiving, nthMonday(y, time.October, 2)},
		{remembranceDay, clock.Date{Year: y, Month: time.November, Day: 11}},
		{christmasDay, clock.Date{Year: y, Month: time.December, Day: 25}},
		{boxingDay, clock.Date{Year: y, Month: time.December, Day: 26}},
	}
}

// easterSunday is the Gregorian Easter Sunday of year y (anonymous Gregorian computus).
func easterSunday(y int) clock.Date {
	a, b, c := y%19, y/100, y%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return clock.Date{Year: y, Month: time.Month(month), Day: day}
}

func weekday(d clock.Date) time.Weekday { return d.Start(time.UTC).Weekday() }

// nthMonday is the n-th Monday of month m in year y.
func nthMonday(y int, m time.Month, n int) clock.Date {
	first := clock.Date{Year: y, Month: m, Day: 1}
	off := (int(time.Monday) - int(weekday(first)) + 7) % 7
	return first.AddDays(off + 7*(n-1))
}

// mondayBefore is the last Monday strictly before d.
func mondayBefore(d clock.Date) clock.Date {
	back := (int(weekday(d)) - int(time.Monday) + 7) % 7
	if back == 0 {
		back = 7
	}
	return d.AddDays(-back)
}

// nextMonday is the first Monday strictly after d.
func nextMonday(d clock.Date) clock.Date {
	fwd := (int(time.Monday) - int(weekday(d)) + 7) % 7
	if fwd == 0 {
		fwd = 7
	}
	return d.AddDays(fwd)
}
