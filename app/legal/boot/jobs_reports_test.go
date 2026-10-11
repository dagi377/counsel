package boot

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/report"
	"github.com/dagi377/counsel/kernel/report/reportfake"
)

var reportNow = time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)

const testReportID = "billing.aged_ar"

// schedules is a fake ScheduleSource.
type schedules struct {
	list []report.Schedule
	err  error
}

func (s schedules) Schedules(context.Context) ([]report.Schedule, error) { return s.list, s.err }

// reportKernel returns a kernel with a reportfake engine holding one report the owner u-1 may
// run, a fake DB and a fake clock.
func reportKernel(t *testing.T) (*kboot.Kernel, *reportfake.Fake) {
	t.Helper()
	clk := clockfake.New(reportNow, time.UTC)
	eng := reportfake.New(clk.Now, reportfake.Caps{"u-1": {"report.run"}})
	err := eng.Register(report.ReportDef{ID: testReportID, Capability: "report.run",
		Run: func(context.Context, pgx.Tx, json.RawMessage) (report.Rows, error) { return report.Rows{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	return &kboot.Kernel{Jobs: kboot.NewJobs(), DB: dbfake.New(), Clock: clk, Reports: eng}, eng
}

func schedule(id string) report.Schedule {
	return report.Schedule{ID: id, ReportID: testReportID, Params: json.RawMessage(`{}`),
		Every: 24 * time.Hour, Anchor: reportNow.Add(-72 * time.Hour), Owner: "u-1"}
}

func TestReportJobsKinds(t *testing.T) {
	tests := []struct {
		name         string
		src          schedules
		wantPeriodic int
		wantErr      bool
	}{
		{name: "two schedules -> two periodic jobs, one worker kind",
			src: schedules{list: []report.Schedule{schedule("weekly-ar"), schedule("daily-wip")}}, wantPeriodic: 2},
		{name: "zero schedules -> worker kind still registered", src: schedules{}, wantPeriodic: 0},
		{name: "store error -> start fails", src: schedules{err: errors.New("db down")}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, _ := reportKernel(t)
			if err := addReportWorker(k); err != nil {
				t.Fatalf("addReportWorker() = %v", err)
			}
			err := startReportSchedules(context.Background(), k, tt.src)
			if (err != nil) != tt.wantErr {
				t.Fatalf("startReportSchedules() error = %v, want error %v", err, tt.wantErr)
			}
			if got := k.Jobs.Kinds(); !reflect.DeepEqual(got, []string{report.JobKindSlot}) {
				t.Fatalf("Kinds() = %v, want [%s]", got, report.JobKindSlot)
			}
			if got := len(k.Jobs.Periodic()); got != tt.wantPeriodic {
				t.Fatalf("periodic jobs = %d, want %d", got, tt.wantPeriodic)
			}
			if err := k.Jobs.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}
		})
	}
}

func TestReportJobsMissingDependency(t *testing.T) {
	tests := []struct {
		name  string
		kern  func(k *kboot.Kernel)
		start bool
		src   ScheduleSource
	}{
		{name: "worker without Reports", kern: func(k *kboot.Kernel) { k.Reports = nil }},
		{name: "worker without DB", kern: func(k *kboot.Kernel) { k.DB = nil }},
		{name: "start without source", start: true, kern: func(*kboot.Kernel) {}},
		{name: "start without clock", start: true, src: schedules{}, kern: func(k *kboot.Kernel) { k.Clock = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, _ := reportKernel(t)
			tt.kern(k)
			var err error
			if tt.start {
				err = startReportSchedules(context.Background(), k, tt.src)
			} else {
				err = addReportWorker(k)
			}
			if !errors.Is(err, ErrJobDependency) {
				t.Fatalf("error = %v, want %v", err, ErrJobDependency)
			}
		})
	}
}

func TestSlotWorkerRunsSlotOnce(t *testing.T) {
	k, eng := reportKernel(t)
	if err := startReportSchedules(context.Background(), k, schedules{list: []report.Schedule{schedule("weekly-ar")}}); err != nil {
		t.Fatal(err)
	}
	slot, err := report.SlotStart(schedule("weekly-ar"), reportNow)
	if err != nil {
		t.Fatal(err)
	}
	w := newSlotWorker(k)
	job := &river.Job[report.SlotArgs]{Args: report.SlotArgs{ScheduleID: "weekly-ar", Slot: slot}}
	tests := []struct {
		name     string
		wantRuns int
	}{
		{name: "first job runs the slot", wantRuns: 1},
		{name: "same slot again runs nothing", wantRuns: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := w.Work(context.Background(), job); err != nil {
				t.Fatalf("Work() = %v", err)
			}
			runs, err := eng.Runs(context.Background(), testReportID)
			if err != nil {
				t.Fatal(err)
			}
			if len(runs) != tt.wantRuns {
				t.Fatalf("runs = %d, want %d", len(runs), tt.wantRuns)
			}
		})
	}
}

func TestSlotWorkerUnknownScheduleFails(t *testing.T) {
	k, _ := reportKernel(t)
	job := &river.Job[report.SlotArgs]{Args: report.SlotArgs{ScheduleID: "nope", Slot: reportNow}}
	if err := newSlotWorker(k).Work(context.Background(), job); !errors.Is(err, report.ErrNotFound) {
		t.Fatalf("Work() = %v, want %v", err, report.ErrNotFound)
	}
}
