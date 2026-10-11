package trigger

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
)

// userList is the active-user source as a fixed list.
type userList struct {
	users []string
	err   error
}

func (u userList) ActiveUsers(context.Context, string) ([]string, error) { return u.users, u.err }

func TestBindPerUser(t *testing.T) {
	cases := []struct {
		name    string
		d       feature.Def
		wantErr bool
	}{
		{"end of day", def("F-TIM-01", feature.SourceJob, "schedule (end of day per user, plus on demand)"), false},
		{"event only has no slot to fan out", def("F-1", feature.SourceJob, "event: document filed"), true},
		{"screen action", def("F-2", feature.SourceScreenAction, "schedule: daily"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := BindPerUser(c.d)
			if c.wantErr {
				if !errors.Is(err, ErrBadBinding) {
					t.Fatalf("err = %v, want ErrBadBinding", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !b.PerUser || b.ActingFor != "" || b.Cadence != CadenceDaily {
				t.Fatalf("binding = %+v", b)
			}
		})
	}
	// A per-user binding never names one fixed user.
	b := Binding{FeatureID: "F-TIM-01", Cadence: CadenceDaily, PerUser: true, ActingFor: "u-admin"}
	if err := b.validate(); !errors.Is(err, ErrBadBinding) {
		t.Fatalf("per-user with ActingFor: err = %v, want ErrBadBinding", err)
	}
}

// rule: LSO-3.6
// Each tick fans out one run per active user, each acting for that user only; a second tick
// in the slot adds nothing; with no user source nothing starts and Readiness reports it.
func TestTickPerUserFansOut(t *testing.T) {
	loc := toronto(t)
	friday := time.Date(2026, 10, 16, 17, 30, 0, 0, loc)
	cases := []struct {
		name        string
		users       ActiveUsers
		wantActing  []string
		wantUnready []Unready
		wantErr     bool
	}{
		{"three timekeepers, three runs", userList{users: []string{"u-c", "u-a", "u-b"}}, []string{"u-a", "u-b", "u-c"}, nil, false},
		{"repeats and empty ids dropped", userList{users: []string{"u-a", "", "u-a"}}, []string{"u-a"}, nil, false},
		{"no active users", userList{}, nil, nil, false},
		{"no user source does not start", nil, nil, []Unready{{FeatureID: "F-TIM-01", Reason: ReasonNoUserSource}}, false},
		{"user source error", userList{err: errors.New("db down")}, nil, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := &fakeQueue{}
			s := New(q, clockfake.New(friday, loc), nil)
			if c.users != nil {
				s.WithUsers(c.users)
			}
			b, err := BindPerUser(def("F-TIM-01", feature.SourceJob, "schedule (end of day per user, plus on demand)"))
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Add(b); err != nil {
				t.Fatal(err)
			}
			_, tickErr := s.Tick(context.Background())
			if c.wantErr {
				if tickErr == nil || len(q.jobs) != 0 {
					t.Fatalf("tick err = %v, runs = %+v; want an error and no run", tickErr, q.jobs)
				}
				return
			}
			if tickErr != nil {
				t.Fatal(tickErr)
			}
			if _, err := s.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			var acting []string
			for _, j := range q.jobs {
				if j.ActingFor != j.ForUser || j.FeatureID != "F-TIM-01" || j.Slot != "2026-10-16" {
					t.Fatalf("run = %+v, want F-TIM-01 in slot 2026-10-16 acting for its own user", j)
				}
				acting = append(acting, j.ActingFor)
			}
			if !slices.Equal(acting, c.wantActing) {
				t.Fatalf("acting for = %v, want %v (one run per user, none repeated)", acting, c.wantActing)
			}
			unready, err := s.Readiness(context.Background())
			if err != nil || !slices.Equal(unready, c.wantUnready) {
				t.Fatalf("unready = %+v (%v), want %+v", unready, err, c.wantUnready)
			}
		})
	}
}

// rule: LSO-3.6
// A run that acts for one user on another user's time is never enqueued.
func TestRunArgsCrossUserRefused(t *testing.T) {
	cases := []struct {
		name    string
		args    RunArgs
		wantErr error
	}{
		{"own time", RunArgs{FeatureID: "F-TIM-01", ActingFor: "u-a", ForUser: "u-a", Slot: "2026-10-16"}, nil},
		{"not per user", RunArgs{FeatureID: "F-CON-05", ActingFor: "u-admin", Slot: "2026-10-16"}, nil},
		{"rule admin on a timekeeper's time", RunArgs{FeatureID: "F-TIM-01", ActingFor: "u-admin", ForUser: "u-a", Slot: "2026-10-16"}, ErrCrossUser},
		{"no acting user", RunArgs{FeatureID: "F-TIM-01", ForUser: "u-a", Slot: "2026-10-16"}, ErrCrossUser},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := &fakeQueue{}
			s := New(q, clockfake.New(time.Now(), toronto(t)), nil)
			err := s.insert(context.Background(), c.args)
			if !errors.Is(err, c.wantErr) || !errors.Is(c.args.CheckOwnUser(), c.wantErr) {
				t.Fatalf("insert err = %v, want %v", err, c.wantErr)
			}
			want := 1
			if c.wantErr != nil {
				want = 0
			}
			if len(q.jobs) != want {
				t.Fatalf("runs = %+v, want %d", q.jobs, want)
			}
		})
	}
}
