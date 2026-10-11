package trigger

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
)

// lawyerMap is firm settings as a map from feature id to designated lawyer.
type lawyerMap struct {
	m   map[string]string
	err error
}

func (l lawyerMap) DesignatedLawyer(_ context.Context, featureID string) (string, error) {
	return l.m[featureID], l.err
}

func TestBindDesignated(t *testing.T) {
	cases := []struct {
		name    string
		d       feature.Def
		events  []string
		wantErr bool
	}{
		{"weekly", def("F-CON-05", feature.SourceJob, "schedule: weekly"), nil, false},
		{"schedule and events", def("F-CON-10", feature.SourceJob, "schedule (daily) and on review date"), []string{"x.y"}, false},
		{"event only has no designated lawyer", def("F-1", feature.SourceJob, "event: document filed"), []string{"x.y"}, true},
		{"screen action", def("F-2", feature.SourceScreenAction, "schedule: weekly"), nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := BindDesignated(c.d, c.events...)
			if c.wantErr {
				if !errors.Is(err, ErrBadBinding) {
					t.Fatalf("err = %v, want ErrBadBinding", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !b.Designated || b.ActingFor != "" || b.Cadence == CadenceNone {
				t.Fatalf("binding = %+v", b)
			}
		})
	}
}

// A scheduled feature acts for its designated lawyer; with none named it does not start
// (fail closed) and Readiness reports it.
func TestTickDesignatedLawyer(t *testing.T) {
	loc := toronto(t)
	monday := time.Date(2026, 10, 19, 9, 0, 0, 0, loc)
	cases := []struct {
		name        string
		lawyers     DesignatedLawyers
		wantActing  []string
		wantUnready []Unready
		wantErr     bool
	}{
		{"designated lawyer acts", lawyerMap{m: map[string]string{"F-CON-05": "u-lawyer"}}, []string{"u-lawyer"}, nil, false},
		{"none named does not start", lawyerMap{m: map[string]string{"F-OTHER-01": "u-lawyer"}}, nil,
			[]Unready{{FeatureID: "F-CON-05", Reason: ReasonNoDesignatedLawyer}}, false},
		{"no settings source does not start", nil, nil,
			[]Unready{{FeatureID: "F-CON-05", Reason: ReasonNoDesignatedLawyer}}, false},
		{"settings read error", lawyerMap{err: errors.New("db down")}, nil, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := &fakeQueue{}
			s := New(q, clockfake.New(monday, loc), nil)
			if c.lawyers != nil {
				s.WithDesignated(c.lawyers)
			}
			b, err := BindDesignated(def("F-CON-05", feature.SourceJob, "schedule: weekly"))
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Add(b); err != nil {
				t.Fatal(err)
			}
			_, tickErr := s.Tick(context.Background())
			unready, readyErr := s.Readiness(context.Background())
			if c.wantErr {
				if tickErr == nil || readyErr == nil {
					t.Fatalf("tick err = %v, readiness err = %v, want errors", tickErr, readyErr)
				}
				if len(q.jobs) != 0 {
					t.Fatalf("runs = %+v, want none", q.jobs)
				}
				return
			}
			if tickErr != nil || readyErr != nil {
				t.Fatalf("tick err = %v, readiness err = %v", tickErr, readyErr)
			}
			var acting []string
			for _, j := range q.jobs {
				acting = append(acting, j.ActingFor)
			}
			if len(acting) != len(c.wantActing) || (len(acting) == 1 && acting[0] != c.wantActing[0]) {
				t.Fatalf("acting for = %v, want %v", acting, c.wantActing)
			}
			if len(unready) != len(c.wantUnready) || (len(unready) == 1 && unready[0] != c.wantUnready[0]) {
				t.Fatalf("unready = %+v, want %+v", unready, c.wantUnready)
			}
		})
	}
}

// Readiness ignores bindings with a fixed acting user and event-only bindings.
func TestReadinessOnlyDesignated(t *testing.T) {
	loc := toronto(t)
	s := New(&fakeQueue{}, clockfake.New(time.Now(), loc), nil)
	fixed, err := Bind(def("F-A-01", feature.SourceJob, "schedule: daily"), "u-1")
	if err != nil {
		t.Fatal(err)
	}
	event, err := Bind(def("F-A-02", feature.SourceJob, "event: upload"), "", "document.uploaded")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []Binding{fixed, event} {
		if err := s.Add(b); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Readiness(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("readiness = %+v, %v; want none", got, err)
	}
}
