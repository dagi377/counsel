package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/outbox"
)

// fakeQueue keeps one job per unique key, as River does for UniqueOpts.ByArgs.
type fakeQueue struct {
	jobs []RunArgs
	seen map[string]bool
}

func (q *fakeQueue) Insert(_ context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	a, ok := args.(RunArgs)
	if !ok {
		return nil, errors.New("unexpected job args")
	}
	if opts == nil || !opts.UniqueOpts.ByArgs {
		return nil, errors.New("run job must be unique by args")
	}
	key := a.FeatureID + "|" + a.EventID + "|" + a.Slot
	if q.seen == nil {
		q.seen = map[string]bool{}
	}
	if q.seen[key] {
		return &rivertype.JobInsertResult{UniqueSkippedAsDuplicate: true}, nil
	}
	q.seen[key] = true
	q.jobs = append(q.jobs, a)
	return &rivertype.JobInsertResult{}, nil
}

func toronto(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func def(id string, src feature.Source, trig string) feature.Def {
	return feature.Def{
		ID: id, Source: src, Trigger: trig,
		Prompt: fstest.MapFS{"p.md": {Data: []byte("prompt")}}, PromptPath: "p.md",
	}
}

func TestParseCadence(t *testing.T) {
	cases := []struct {
		trigger string
		want    Cadence
	}{
		{"schedule: weekly", CadenceWeekly},
		{"schedule daily", CadenceDaily},
		{"schedule: nightly", CadenceDaily},
		{"schedule (daily) and on role or grant change", CadenceDaily},
		{"on invite and monthly", CadenceMonthly},
		{"schedule (quarterly)", CadenceQuarterly},
		{"schedule: hourly", CadenceHourly},
		{"schedule: weekdays 07:30, Mondays 07:00", CadenceDaily},
		{"schedule: Monday 08:00 America/Toronto", CadenceWeekly},
		{"event: lead awaiting info; schedule", CadenceDaily},
		{"event: G-B1 closed", CadenceNone},
		// A sweep is a schedule word.
		{"event: matter opened without a template; weekly sweep", CadenceWeekly},
		{"nightly sweep", CadenceDaily},
		{"sweep", CadenceDaily},
		{"Sweep (quarterly)", CadenceQuarterly},
		{"event: matter opened without a template", CadenceNone},
	}
	for _, c := range cases {
		if got := ParseCadence(c.trigger); got != c.want {
			t.Errorf("ParseCadence(%q) = %q, want %q", c.trigger, got, c.want)
		}
	}
}

func TestBind(t *testing.T) {
	cases := []struct {
		name    string
		d       feature.Def
		acting  string
		events  []string
		wantErr bool
		want    Binding
	}{
		{"event only", def("F-1", feature.SourceJob, "event: document filed"), "", []string{"document.uploaded", "document.uploaded"}, false,
			Binding{FeatureID: "F-1", Events: []string{"document.uploaded"}}},
		{"schedule", def("F-2", feature.SourceJob, "schedule: daily"), "u-1", nil, false,
			Binding{FeatureID: "F-2", Cadence: CadenceDaily, ActingFor: "u-1"}},
		{"screen action", def("F-3", feature.SourceScreenAction, "user asks"), "", []string{"x.y"}, true, Binding{}},
		{"nothing fires it", def("F-4", feature.SourceJob, "event"), "", nil, true, Binding{}},
		{"schedule without acting user", def("F-5", feature.SourceJob, "schedule: weekly"), "", nil, true, Binding{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Bind(c.d, c.acting, c.events...)
			if c.wantErr {
				if !errors.Is(err, ErrBadBinding) {
					t.Fatalf("err = %v, want ErrBadBinding", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.FeatureID != c.want.FeatureID || got.Cadence != c.want.Cadence || got.ActingFor != c.want.ActingFor ||
				strings.Join(got.Events, ",") != strings.Join(c.want.Events, ",") {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestOnEvent(t *testing.T) {
	loc := toronto(t)
	msg := func(id, typ string, payload string) outbox.Message {
		return outbox.Message{ID: id, Type: typ, Subject: outbox.Subject{Kind: "matter", ID: "m-1"}, Payload: json.RawMessage(payload)}
	}
	cases := []struct {
		name     string
		events   []outbox.Message
		wantRuns int
		wantErr  error
	}{
		{"one event one run", []outbox.Message{msg("e-1", "document.uploaded", `{"acting_for":"u-1"}`)}, 1, nil},
		// rule: AI-R-005
		{"duplicate event id one run", []outbox.Message{
			msg("e-1", "document.uploaded", `{"acting_for":"u-1"}`),
			msg("e-1", "document.uploaded", `{"acting_for":"u-1"}`),
		}, 1, nil},
		{"two events two runs", []outbox.Message{
			msg("e-1", "document.uploaded", `{"user_id":"u-1"}`),
			msg("e-2", "document.uploaded", `{"user_id":"u-1"}`),
		}, 2, nil},
		{"unbound event", []outbox.Message{msg("e-1", "bill.delivered", `{}`)}, 0, nil},
		{"no acting user", []outbox.Message{msg("e-1", "document.uploaded", `{"amount_cents":10}`)}, 0, ErrNoActingFor},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := &fakeQueue{}
			s := New(q, clockfake.New(time.Date(2026, 10, 13, 9, 0, 0, 0, loc), loc), nil)
			b, err := Bind(def("F-DOC-01", feature.SourceJob, "event: upload"), "", "document.uploaded")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Add(b); err != nil {
				t.Fatal(err)
			}
			for _, m := range c.events {
				_, err = s.OnEvent(context.Background(), m)
				if err != nil {
					break
				}
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if len(q.jobs) != c.wantRuns {
				t.Fatalf("runs = %d, want %d", len(q.jobs), c.wantRuns)
			}
			for _, j := range q.jobs {
				// rule: AI-R-005
				// Job args hold ids only: no payload, prompt or client text.
				raw, _ := json.Marshal(j)
				var fields map[string]any
				_ = json.Unmarshal(raw, &fields)
				for k := range fields {
					switch k {
					case "feature_id", "subjects", "acting_for", "event_id", "slot":
					default:
						t.Fatalf("job arg %q is not an id", k)
					}
				}
				if j.ActingFor != "u-1" || len(j.Subjects) != 1 || j.Subjects[0] != (SubjectRef{"matter", "m-1"}) {
					t.Fatalf("job = %+v", j)
				}
			}
		})
	}
}

func TestTickSchedules(t *testing.T) {
	loc := toronto(t)
	thanksgiving := clock.Date{Year: 2026, Month: time.October, Day: 12} // Monday holiday
	cases := []struct {
		name     string
		cadence  string
		holidays []clock.Date
		from     time.Time
		days     int
		want     []string
	}{
		{"daily fires once per business day", "schedule: daily", nil,
			time.Date(2026, 10, 12, 0, 0, 0, 0, loc), 7,
			[]string{"2026-10-12", "2026-10-13", "2026-10-14", "2026-10-15", "2026-10-16"}},
		{"daily skips holiday", "schedule: daily", []clock.Date{thanksgiving},
			time.Date(2026, 10, 12, 0, 0, 0, 0, loc), 7,
			[]string{"2026-10-13", "2026-10-14", "2026-10-15", "2026-10-16"}},
		{"weekly fires first business day", "schedule: weekly", []clock.Date{thanksgiving},
			time.Date(2026, 10, 10, 0, 0, 0, 0, loc), 14,
			[]string{"2026-10-12", "2026-10-19"}},
		{"monthly fires once", "schedule: monthly", nil,
			time.Date(2026, 10, 28, 0, 0, 0, 0, loc), 10,
			[]string{"2026-11-01"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := &fakeQueue{}
			clk := clockfake.New(c.from, loc)
			s := New(q, clk, c.holidays)
			b, err := Bind(def("F-X", feature.SourceJob, c.cadence), "u-1")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Add(b); err != nil {
				t.Fatal(err)
			}
			// Tick every 15 minutes, as the periodic job does.
			for i := 0; i < c.days*24*4; i++ {
				if _, err := s.Tick(context.Background()); err != nil {
					t.Fatal(err)
				}
				clk.Advance(TickInterval)
			}
			var got []string
			for _, j := range q.jobs {
				if j.FeatureID != "F-X" || j.ActingFor != "u-1" || j.EventID != "" {
					t.Fatalf("job = %+v", j)
				}
				got = append(got, j.Slot)
			}
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("slots = %v, want %v", got, c.want)
			}
		})
	}
}

func TestAddRejectsDuplicateFeature(t *testing.T) {
	loc := toronto(t)
	s := New(&fakeQueue{}, clockfake.New(time.Now(), loc), nil)
	b := Binding{FeatureID: "F-1", Events: []string{"gate.decided"}}
	if err := s.Add(b); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(b); !errors.Is(err, ErrBadBinding) {
		t.Fatalf("err = %v, want ErrBadBinding", err)
	}
	if got := s.EventTypes(); len(got) != 1 || got[0] != "gate.decided" {
		t.Fatalf("event types = %v", got)
	}
}
