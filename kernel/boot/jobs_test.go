package boot

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
)

type pingArgs struct{}

func (pingArgs) Kind() string { return "test.ping" }

type sweepArgs struct{}

func (sweepArgs) Kind() string { return "test.sweep" }

type pingWorker struct{ river.WorkerDefaults[pingArgs] }

func (pingWorker) Work(context.Context, *river.Job[pingArgs]) error { return nil }

type sweepWorker struct {
	river.WorkerDefaults[sweepArgs]
}

func (sweepWorker) Work(context.Context, *river.Job[sweepArgs]) error { return nil }

func hourly(kind string) *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(time.Hour), func() (river.JobArgs, *river.InsertOpts) {
		if kind == "test.ping" {
			return pingArgs{}, nil
		}
		return sweepArgs{}, nil
	}, nil)
}

func TestJobs(t *testing.T) {
	tests := []struct {
		name        string
		build       func(j *Jobs) error
		wantKinds   []string
		wantAddErr  string // substring of the AddWorker error; empty means none
		wantInvalid bool
	}{
		{name: "empty set is valid", build: func(*Jobs) error { return nil }, wantKinds: []string{}},
		{name: "workers sorted by kind", build: func(j *Jobs) error {
			if err := AddWorker[sweepArgs](j, sweepWorker{}); err != nil {
				return err
			}
			return AddWorker[pingArgs](j, pingWorker{})
		}, wantKinds: []string{"test.ping", "test.sweep"}},
		{name: "second worker of one kind", build: func(j *Jobs) error {
			if err := AddWorker[pingArgs](j, pingWorker{}); err != nil {
				return err
			}
			return AddWorker[pingArgs](j, pingWorker{})
		}, wantKinds: []string{"test.ping"}, wantAddErr: `"test.ping"`},
		{name: "periodic job with worker", build: func(j *Jobs) error {
			j.AddPeriodic(hourly("test.ping"), "test.ping")
			return AddWorker[pingArgs](j, pingWorker{})
		}, wantKinds: []string{"test.ping"}},
		{name: "periodic job without worker", build: func(j *Jobs) error {
			j.AddPeriodic(hourly("test.sweep"), "test.sweep")
			return AddWorker[pingArgs](j, pingWorker{})
		}, wantKinds: []string{"test.ping"}, wantInvalid: true},
		{name: "nil periodic job", build: func(j *Jobs) error {
			j.AddPeriodic(nil, "test.ping")
			return AddWorker[pingArgs](j, pingWorker{})
		}, wantKinds: []string{"test.ping"}, wantInvalid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := NewJobs()
			err := tt.build(j)
			switch {
			case tt.wantAddErr == "" && err != nil:
				t.Fatalf("build: %v", err)
			case tt.wantAddErr != "" && (!errors.Is(err, ErrJobs) || !strings.Contains(err.Error(), tt.wantAddErr)):
				t.Fatalf("build err = %v, want %v naming %s", err, ErrJobs, tt.wantAddErr)
			}
			if got := j.Kinds(); !reflect.DeepEqual(got, tt.wantKinds) {
				t.Fatalf("Kinds() = %v, want %v", got, tt.wantKinds)
			}
			if err := j.Validate(); (err != nil) != tt.wantInvalid || (err != nil && !errors.Is(err, ErrJobs)) {
				t.Fatalf("Validate() = %v, want invalid=%v", err, tt.wantInvalid)
			}
		})
	}
}

func TestBuildWithJobs(t *testing.T) {
	app := App{Name: "test", Register: func(_ context.Context, k *Kernel) error {
		k.Jobs.AddPeriodic(hourly("test.ping"), "test.ping")
		return AddWorker[pingArgs](k.Jobs, pingWorker{})
	}}
	k, err := BuildWith(context.Background(), Deps{}, nil, app)
	if err != nil {
		t.Fatalf("BuildWith() = %v", err)
	}
	if got := k.Jobs.Kinds(); !reflect.DeepEqual(got, []string{"test.ping"}) {
		t.Fatalf("Kinds() = %v", got)
	}
	if got := len(k.Jobs.Periodic()); got != 1 {
		t.Fatalf("Periodic() has %d jobs, want 1", got)
	}
	if err := k.Jobs.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}
