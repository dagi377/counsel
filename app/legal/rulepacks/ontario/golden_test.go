package ontario

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
	"github.com/dagi377/counsel/kernel/registry"
)

const civilProcedureFile = "civil-procedure.yaml"

// loadCivilProcedure parses civil-procedure.yaml with the kernel rule-pack loader, which
// validates it against spec/kernel/rulepack.schema.json.
func loadCivilProcedure(t *testing.T) deadline.Pack {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "spec", "kernel", "rulepack.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	l, err := deadline.NewPackLoader(schema)
	if err != nil {
		t.Fatalf("loader: %v", err)
	}
	raw, err := os.ReadFile(civilProcedureFile)
	if err != nil {
		t.Fatalf("read %s: %v", civilProcedureFile, err)
	}
	p, err := l.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", civilProcedureFile, err)
	}
	return p
}

func d(y int, m time.Month, day int) clock.Date { return clock.Date{Year: y, Month: m, Day: day} }

// TestCivilProcedureGoldenDates checks hand-computed due dates for every pack entry, each at
// least once on an ordinary day and once where a weekend or holiday moves the date.
// rule: r. 3.01 (verify)
func TestCivilProcedureGoldenDates(t *testing.T) {
	p := loadCivilProcedure(t)
	e := deadlinefake.New()
	ctx := context.Background()
	if err := e.LoadPack(ctx, p); err != nil {
		t.Fatalf("load pack: %v", err)
	}
	tests := []struct {
		name    string
		rule    string
		trigger clock.Date
		want    clock.Date
	}{
		{"defence plain lands sunday rolls", "statement_of_defence", d(2026, time.March, 2), d(2026, time.March, 23)},
		{"defence lands on monday after christmas", "statement_of_defence", d(2026, time.December, 8), d(2026, time.December, 29)},
		{"intent to defend plain", "notice_of_intent_to_defend", d(2026, time.March, 2), d(2026, time.March, 12)},
		{"intent to defend lands easter monday", "notice_of_intent_to_defend", d(2026, time.March, 27), d(2026, time.April, 7)},
		{"reply plain", "reply", d(2026, time.September, 28), d(2026, time.October, 8)},
		{"reply lands thanksgiving", "reply", d(2026, time.October, 2), d(2026, time.October, 13)},
		{"appeal lands canada day", "notice_of_appeal", d(2026, time.June, 1), d(2026, time.July, 2)},
		{"appeal lands saturday", "notice_of_appeal", d(2026, time.October, 1), d(2026, time.November, 2)},
		{"leave plain", "leave_to_appeal_motion", d(2026, time.July, 20), d(2026, time.August, 4)},
		{"leave lands christmas run", "leave_to_appeal_motion", d(2026, time.December, 10), d(2026, time.December, 29)},
		{"short period plain week", "short_period_7_days", d(2026, time.March, 2), d(2026, time.March, 11)},
		{"short period over easter", "short_period_7_days", d(2026, time.April, 1), d(2026, time.April, 14)},
		{"short period over victoria day", "short_period_7_days", d(2026, time.May, 14), d(2026, time.May, 26)},
	}
	covered := map[string]bool{}
	for _, tc := range tests {
		covered[tc.rule] = true
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.Compute(ctx, p.ID, tc.rule, tc.trigger, registry.Subject{})
			if err != nil {
				t.Fatalf("compute: %v", err)
			}
			if len(got) != 1 || got[0].Due != tc.want {
				t.Fatalf("%s from %s = %+v, want %s", tc.rule, tc.trigger, got, tc.want)
			}
		})
	}
	for _, r := range p.Rules {
		if !covered[r.ID] {
			t.Errorf("rule %s has no golden date", r.ID)
		}
	}
}

// TestCivilProcedureHolidaysMatchCalendar keeps the pack's holidays equal to holidays.yaml.
func TestCivilProcedureHolidaysMatchCalendar(t *testing.T) {
	cp, cal := loadCivilProcedure(t), loadHolidays(t)
	if len(cp.Holidays) != len(cal.Holidays) {
		t.Fatalf("civil procedure has %d holidays, calendar has %d", len(cp.Holidays), len(cal.Holidays))
	}
	for i := range cal.Holidays {
		if cp.Holidays[i] != cal.Holidays[i] {
			t.Errorf("holiday %d = %s, calendar has %s", i, cp.Holidays[i], cal.Holidays[i])
		}
	}
}
