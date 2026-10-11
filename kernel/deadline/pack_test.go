package deadline_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
)

func loader(t *testing.T) *deadline.PackLoader {
	t.Helper()
	schema, err := os.ReadFile("../../spec/kernel/rulepack.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	l, err := deadline.NewPackLoader(schema)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func helpdesk(t *testing.T) deadline.Pack {
	t.Helper()
	raw, err := os.ReadFile("../../test/fixtures/helpdesk/rulepack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	p, err := loader(t).Parse(raw)
	if err != nil {
		t.Fatalf("parse helpdesk pack: %v", err)
	}
	return p
}

func date(y int, m time.Month, d int) deadline.Date { return deadline.Date{Year: y, Month: m, Day: d} }

func TestParseHelpdeskPack(t *testing.T) {
	want := deadline.Pack{
		ID:       "helpdesk_sla",
		Version:  "1",
		Holidays: []deadline.Date{date(2026, 12, 25), date(2027, 1, 1)},
		Rules: []deadline.Rule{
			{ID: "first_response", Trigger: "ticket_opened", Mode: deadline.Business, Days: 1, Roll: deadline.RollForward},
			{ID: "auto_close", Trigger: "customer_silent", Mode: deadline.Calendar, Days: 14, Roll: deadline.RollNone},
		},
	}
	if got := helpdesk(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("pack =\n%+v\nwant\n%+v", got, want)
	}
}

func TestHelpdeskPackGoldenDates(t *testing.T) {
	e := deadlinefake.New()
	ctx := context.Background()
	if err := e.LoadPack(ctx, helpdesk(t)); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		rule    string
		trigger deadline.Date
		want    deadline.Date
	}{
		{"next business day", "first_response", date(2026, 12, 22), date(2026, 12, 23)},
		{"skips holiday and weekend", "first_response", date(2026, 12, 24), date(2026, 12, 28)},
		{"skips new year holiday", "first_response", date(2026, 12, 31), date(2027, 1, 4)},
		{"calendar days, no roll onto Monday", "auto_close", date(2026, 12, 20), date(2027, 1, 3)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.Compute(ctx, "helpdesk_sla", tc.rule, tc.trigger, deadline.Subject{})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Due != tc.want {
				t.Fatalf("due = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseRejectsInvalidPacks(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"not yaml", "pack: [a"},
		{"missing rules", "pack: p\nversion: 1\n"},
		{"empty rules", "pack: p\nversion: 1\nrules: []\n"},
		{"version not integer", "pack: p\nversion: one\nrules: [{id: r, trigger: t, offset_days: 1, day_count: calendar}]\n"},
		{"version zero", "pack: p\nversion: 0\nrules: [{id: r, trigger: t, offset_days: 1, day_count: calendar}]\n"},
		{"bad pack id", "pack: P-1\nversion: 1\nrules: [{id: r, trigger: t, offset_days: 1, day_count: calendar}]\n"},
		{"unknown field", "pack: p\nversion: 1\nowner: x\nrules: [{id: r, trigger: t, offset_days: 1, day_count: calendar}]\n"},
		{"unknown day count", "pack: p\nversion: 1\nrules: [{id: r, trigger: t, offset_days: 1, day_count: lunar}]\n"},
		{"bad holiday", "pack: p\nversion: 1\nholidays: [2026-13-40]\nrules: [{id: r, trigger: t, offset_days: 1, day_count: calendar}]\n"},
		{"duplicate holiday", "pack: p\nversion: 1\nholidays: [\"2026-12-25\", \"2026-12-25\"]\nrules: [{id: r, trigger: t, offset_days: 1, day_count: calendar}]\n"},
		{"bad time zone", "pack: p\nversion: 1\ntime_zone: Mars/Base\nrules: [{id: r, trigger: t, offset_days: 1, day_count: calendar}]\n"},
		{"duplicate rule", "pack: p\nversion: 1\nrules: [{id: r, trigger: t, offset_days: 1, day_count: calendar}, {id: r, trigger: t, offset_days: 2, day_count: calendar}]\n"},
		{"backward roll unsupported", "pack: p\nversion: 1\nrules: [{id: r, trigger: t, offset_days: 1, day_count: calendar, roll: backward}]\n"},
		{"missing offset", "pack: p\nversion: 1\nrules: [{id: r, trigger: t, day_count: calendar}]\n"},
	}
	l := loader(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := l.Parse([]byte(tc.yaml)); !errors.Is(err, deadline.ErrInvalidPack) {
				t.Fatalf("err = %v, want ErrInvalidPack", err)
			}
		})
	}
}

func TestParseOptionalFields(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want deadline.Rule
	}{
		{"roll defaults to none", "pack: p\nversion: 2\nrules: [{id: r, trigger: t, offset_days: 3, day_count: business}]\n",
			deadline.Rule{ID: "r", Trigger: "t", Mode: deadline.Business, Days: 3, Roll: deadline.RollNone}},
		{"unquoted holiday dates", "pack: p\nversion: 2\nholidays: [2026-07-01]\nrules: [{id: r, trigger: t, offset_days: 0, day_count: calendar, roll: forward, confirm: true}]\n",
			deadline.Rule{ID: "r", Trigger: "t", Mode: deadline.Calendar, Days: 0, Roll: deadline.RollForward}},
	}
	l := loader(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := l.Parse([]byte(tc.yaml))
			if err != nil {
				t.Fatal(err)
			}
			if p.Version != "2" || len(p.Rules) != 1 || p.Rules[0] != tc.want {
				t.Fatalf("pack = %+v, want version 2 and rule %+v", p, tc.want)
			}
		})
	}
}
