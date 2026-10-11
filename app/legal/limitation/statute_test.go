package limitation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/app/legal/limitation"
)

func d(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &t
}

func susp(k limitation.SuspensionKind, from, to string) limitation.Suspension {
	s := limitation.Suspension{Kind: k, From: *d(from)}
	if to != "" {
		s.To = d(to)
	}
	return s
}

type schedule map[string]limitation.SchedulePeriod

func (s schedule) Lookup(_ context.Context, claimType string) (limitation.SchedulePeriod, bool, error) {
	p, ok := s[claimType]
	return p, ok, nil
}

type failingSchedule struct{}

func (failingSchedule) Lookup(context.Context, string) (limitation.SchedulePeriod, bool, error) {
	return limitation.SchedulePeriod{}, false, errors.New("schedule down")
}

func newStatute(t *testing.T, sch limitation.Schedule) *limitation.Statute {
	t.Helper()
	s, err := limitation.NewStatute(sch)
	if err != nil {
		t.Fatalf("NewStatute: %v", err)
	}
	return s
}

func got(ps []limitation.Period) map[string]string {
	out := map[string]string{}
	for _, p := range ps {
		out[p.Kind] = p.Due.Format("2006-01-02")
	}
	return out
}

func TestLimitationsAct(t *testing.T) {
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-09-28T02:30Z is 2026-09-27 22:30 in Toronto.
	lateEvening := time.Date(2026, 9, 28, 2, 30, 0, 0, time.UTC)
	sch := schedule{"scheduled-claim": {Provision: "Example Act, s. 1", Months: 6}}

	tests := []struct {
		name string
		a    limitation.Assessment
		want map[string]string
		err  error
	}{
		// s.4 and s.5(2): the spec's worked example (fall on a sidewalk, presumed discovery).
		{"s4_s5_2_presumed_discovery", limitation.Assessment{MatterID: "m", ActDate: d("2026-09-28"), DiscoveryPresumed: true},
			map[string]string{"basic": "2028-09-28", "ultimate": "2041-09-28"}, nil},
		// s.5(1): the contrary is proved, so the basic period runs from the later discovery.
		{"s4_s5_1_proved_discovery", limitation.Assessment{LeadID: "l", ActDate: d("2020-03-15"), DiscoveryDate: d("2021-06-01")},
			map[string]string{"basic": "2023-06-01", "ultimate": "2035-03-15"}, nil},
		{"s5_2_presumed_with_same_discovery_date", limitation.Assessment{MatterID: "m", ActDate: d("2026-09-28"), DiscoveryDate: d("2026-09-28"), DiscoveryPresumed: true},
			map[string]string{"basic": "2028-09-28", "ultimate": "2041-09-28"}, nil},
		{"s5_2_presumed_conflicts_with_discovery_date", limitation.Assessment{MatterID: "m", ActDate: d("2026-09-28"), DiscoveryDate: d("2026-10-01"), DiscoveryPresumed: true},
			nil, limitation.ErrDates},
		{"s5_neither_proved_nor_presumed", limitation.Assessment{MatterID: "m", ActDate: d("2026-09-28")}, nil, limitation.ErrDates},
		{"s5_discovery_before_act", limitation.Assessment{MatterID: "m", ActDate: d("2026-09-28"), DiscoveryDate: d("2026-09-01")}, nil, limitation.ErrDates},
		{"s15_act_date_missing", limitation.Assessment{MatterID: "m", DiscoveryDate: d("2026-09-28")}, nil, limitation.ErrDates},
		// s.15(2): the ultimate period bars the claim before the basic period ends.
		{"s15_2_ultimate_earlier_than_basic", limitation.Assessment{MatterID: "m", ActDate: d("2000-01-01"), DiscoveryDate: d("2014-12-01")},
			map[string]string{"basic": "2016-12-01", "ultimate": "2015-01-01"}, nil},
		// Legislation Act s.89 (verify): Feb 29 anniversaries in common years fall on Feb 28.
		{"leap_day_anniversary_feb28", limitation.Assessment{MatterID: "m", ActDate: d("2024-02-29"), DiscoveryPresumed: true},
			map[string]string{"basic": "2026-02-28", "ultimate": "2039-02-28"}, nil},
		{"leap_day_anniversary_in_leap_year", limitation.Assessment{MatterID: "m", ActDate: d("2024-02-29"), DiscoveryDate: d("2026-02-28")},
			map[string]string{"basic": "2028-02-28", "ultimate": "2039-02-28"}, nil},
		// America/Toronto dates: an instant late in the Toronto evening counts on that day.
		{"toronto_calendar_date_from_instant", limitation.Assessment{MatterID: "m", ActDate: &lateEvening, DiscoveryPresumed: true},
			map[string]string{"basic": "2028-09-27", "ultimate": "2041-09-27"}, nil},
		// s.6 and s.15(4)(b): a minor without a litigation guardian; both periods start on
		// the 18th birthday.
		{"s6_s15_4_b_minor_until_18", limitation.Assessment{MatterID: "m", ActDate: d("2020-05-10"), DiscoveryPresumed: true,
			Suspensions: []limitation.Suspension{susp(limitation.SuspendMinority, "2007-07-01", "2025-07-01")}},
			map[string]string{"basic": "2027-07-01", "ultimate": "2040-07-01"}, nil},
		// s.7 and s.15(4)(a): 30 days of incapacity inside both periods extend each by 30 days.
		{"s7_s15_4_a_incapacity_mid_period", limitation.Assessment{MatterID: "m", ActDate: d("2020-01-01"), DiscoveryPresumed: true,
			Suspensions: []limitation.Suspension{susp(limitation.SuspendIncapacity, "2021-01-01", "2021-01-31")}},
			map[string]string{"basic": "2022-01-31", "ultimate": "2035-01-31"}, nil},
		// s.7: a second incapacity reached only after the first extension also counts.
		{"s7_incapacity_reached_by_extension", limitation.Assessment{MatterID: "m", ActDate: d("2020-01-01"), DiscoveryPresumed: true,
			Suspensions: []limitation.Suspension{
				susp(limitation.SuspendIncapacity, "2021-01-01", "2021-01-31"),
				susp(limitation.SuspendIncapacity, "2022-01-20", "2022-01-25"),
			}},
			map[string]string{"basic": "2022-02-05", "ultimate": "2035-02-05"}, nil},
		// s.6 and s.15(4): a minor still without a guardian; no period runs yet.
		{"s6_open_minority_suspends_all", limitation.Assessment{MatterID: "m", ActDate: d("2020-05-10"), DiscoveryPresumed: true,
			Suspensions: []limitation.Suspension{susp(limitation.SuspendMinority, "2010-01-01", "")}},
			nil, limitation.ErrSuspended},
		// s.15(4)(c): wilful concealment suspends only the ultimate period; the basic period
		// runs from the proved discovery (s.5).
		{"s15_4_c_concealment", limitation.Assessment{MatterID: "m", ActDate: d("2010-01-01"), DiscoveryDate: d("2024-06-01"),
			Suspensions: []limitation.Suspension{susp(limitation.SuspendConcealment, "2010-01-01", "2024-06-01")}},
			map[string]string{"basic": "2026-06-01", "ultimate": "2039-06-01"}, nil},
		{"s15_4_c_concealment_continuing", limitation.Assessment{MatterID: "m", ActDate: d("2010-01-01"), DiscoveryDate: d("2024-06-01"),
			Suspensions: []limitation.Suspension{susp(limitation.SuspendConcealment, "2010-01-01", "")}},
			map[string]string{"basic": "2026-06-01"}, nil},
		// s.22 (verify): an agreement suspends both periods.
		{"s22_agreement_suspends_both", limitation.Assessment{MatterID: "m", ActDate: d("2020-01-01"), DiscoveryPresumed: true,
			Suspensions: []limitation.Suspension{susp(limitation.SuspendAgreement, "2021-03-01", "2021-03-11")}},
			map[string]string{"basic": "2022-01-11", "ultimate": "2035-01-11"}, nil},
		{"suspension_ends_before_start", limitation.Assessment{MatterID: "m", ActDate: d("2020-01-01"), DiscoveryPresumed: true,
			Suspensions: []limitation.Suspension{susp(limitation.SuspendIncapacity, "2021-03-11", "2021-03-01")}},
			nil, limitation.ErrDates},
		// s.16(1)(h) and (h.1): no limitation period, even with no dates.
		{"s16_1_h_sexual_assault", limitation.Assessment{MatterID: "m", NoLimit: limitation.NoLimitSexualAssault}, nil, limitation.ErrNoPeriod},
		{"s16_1_h1_sexual_misconduct", limitation.Assessment{MatterID: "m", ActDate: d("1990-01-01"), DiscoveryPresumed: true,
			NoLimit: limitation.NoLimitSexualMisconduct}, nil, limitation.ErrNoPeriod},
		{"s16_unknown_kind", limitation.Assessment{MatterID: "m", NoLimit: "made_up"}, nil, limitation.ErrNoPeriod},
		// s.17 (verify): an undiscovered environmental claim has no period; once discovered,
		// only the basic period runs.
		{"s17_environmental_undiscovered", limitation.Assessment{MatterID: "m", ActDate: d("1995-01-01"), NoLimit: limitation.NoLimitEnvironmental},
			nil, limitation.ErrNoPeriod},
		{"s17_environmental_discovered", limitation.Assessment{MatterID: "m", ActDate: d("1995-01-01"), DiscoveryDate: d("2025-04-01"), NoLimit: limitation.NoLimitEnvironmental},
			map[string]string{"basic": "2027-04-01"}, nil},
		// s.18: contribution and indemnity runs from service of the claim, which is deemed
		// the day of the act or omission for s.5(2) and s.15.
		{"s18_contribution_from_service", limitation.Assessment{MatterID: "m", ActDate: d("2019-06-01"), ContributionServedDate: d("2025-03-03"), DiscoveryPresumed: true},
			map[string]string{"basic": "2027-03-03", "ultimate": "2040-03-03"}, nil},
		{"s18_contribution_later_discovery", limitation.Assessment{MatterID: "m", ContributionServedDate: d("2025-03-03"), DiscoveryDate: d("2025-05-01")},
			map[string]string{"basic": "2027-05-01", "ultimate": "2040-03-03"}, nil},
		// s.19: a Schedule period replaces the basic and ultimate periods.
		{"s19_schedule_period", limitation.Assessment{MatterID: "m", ClaimType: "scheduled-claim", ActDate: d("2026-08-31"), DiscoveryPresumed: true},
			map[string]string{"schedule": "2027-02-28"}, nil},
		{"s19_unlisted_claim_type_uses_act", limitation.Assessment{MatterID: "m", ClaimType: "negligence", ActDate: d("2026-08-31"), DiscoveryPresumed: true},
			map[string]string{"basic": "2028-08-31", "ultimate": "2041-08-31"}, nil},
		{"owner_both", limitation.Assessment{MatterID: "m", LeadID: "l", ActDate: d("2026-09-28"), DiscoveryPresumed: true}, nil, limitation.ErrOwner},
		{"owner_neither", limitation.Assessment{ActDate: d("2026-09-28"), DiscoveryPresumed: true}, nil, limitation.ErrOwner},
	}
	s := newStatute(t, sch)
	ctx := legalflagstest.Context(t, legalflags.DeadlineOntarioPack, true)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps, err := s.Compute(ctx, tc.a)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err = %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Compute: %v", err)
			}
			g := got(ps)
			if len(g) != len(tc.want) {
				t.Fatalf("periods = %v, want %v", g, tc.want)
			}
			for k, v := range tc.want {
				if g[k] != v {
					t.Errorf("%s = %s, want %s (all %v)", k, g[k], v, g)
				}
			}
			for _, p := range ps {
				if p.Due.Location().String() != toronto.String() {
					t.Errorf("%s due in %s, want America/Toronto", p.Kind, p.Due.Location())
				}
				if h, m, sec := p.Due.Clock(); h != 0 || m != 0 || sec != 0 {
					t.Errorf("%s due %v is not midnight", p.Kind, p.Due)
				}
			}
		})
	}
}

func TestLimitationsActFlagFailsClosed(t *testing.T) {
	a := limitation.Assessment{MatterID: "m", ActDate: d("2026-09-28"), DiscoveryPresumed: true}
	s := newStatute(t, nil)
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{"flag_off", legalflagstest.Context(t, legalflags.DeadlineOntarioPack, false)},
		{"flag_undefined", context.Background()},
		{"other_flag_on", legalflagstest.Context(t, legalflags.TaxVerifyRules, true)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps, err := s.Compute(tc.ctx, a)
			if !errors.Is(err, limitation.ErrUnverified) || ps != nil {
				t.Fatalf("Compute = %v, %v; want nil, ErrUnverified", ps, err)
			}
		})
	}
}

func TestLimitationsActScheduleErrors(t *testing.T) {
	ctx := legalflagstest.Context(t, legalflags.DeadlineOntarioPack, true)
	tests := []struct {
		name string
		sch  limitation.Schedule
		a    limitation.Assessment
		err  error
	}{
		{"lookup_error", failingSchedule{}, limitation.Assessment{MatterID: "m", ClaimType: "x", ActDate: d("2026-01-01"), DiscoveryPresumed: true}, nil},
		{"empty_period", schedule{"x": {Provision: "Empty"}}, limitation.Assessment{MatterID: "m", ClaimType: "x", ActDate: d("2026-01-01"), DiscoveryPresumed: true}, limitation.ErrNoPeriod},
		{"from_discovery_without_discovery", schedule{"x": {Provision: "P", Years: 1, FromDiscovery: true}},
			limitation.Assessment{MatterID: "m", ClaimType: "x", ActDate: d("2026-01-01")}, limitation.ErrDates},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newStatute(t, tc.sch).Compute(ctx, tc.a)
			if err == nil {
				t.Fatal("Compute: want error")
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
	// A Schedule period from discovery, with years, months and days.
	sch := schedule{"x": {Provision: "P", Years: 1, Months: 1, Days: 3, FromDiscovery: true}}
	ps, err := newStatute(t, sch).Compute(ctx, limitation.Assessment{MatterID: "m", ClaimType: "x", ActDate: d("2025-12-01"), DiscoveryDate: d("2026-01-31")})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if g := got(ps); g["schedule"] != "2027-03-03" || len(g) != 1 {
		t.Fatalf("periods = %v, want schedule 2027-03-03", g)
	}
}
