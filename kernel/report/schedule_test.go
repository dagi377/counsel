package report_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/kernel/report"
)

func newScheduler(t *testing.T) (*report.Reports, *report.Reports) {
	t.Helper()
	e, _ := newEngine(t)
	s := e
	sc := report.Schedule{
		ID: "sch-1", ReportID: "ops.aging", Params: json.RawMessage(`{"period":"2026-09"}`),
		Every: 24 * time.Hour, Anchor: t0, Owner: "ann",
	}
	if err := s.AddSchedule(context.Background(), nil, sc); err != nil {
		t.Fatal(err)
	}
	return e, s
}

func TestAddSchedule(t *testing.T) {
	ok := report.Schedule{ID: "sch-2", ReportID: "ops.aging", Params: json.RawMessage(`{"period":"2026-09"}`), Every: time.Hour, Anchor: t0, Owner: "ann"}
	mod := func(f func(*report.Schedule)) report.Schedule { s := ok; f(&s); return s }
	tests := []struct {
		name string
		sc   report.Schedule
		want error
	}{
		{"valid", ok, nil},
		{"duplicate id", mod(func(s *report.Schedule) { s.ID = "sch-1" }), report.ErrDuplicate},
		{"unknown report", mod(func(s *report.Schedule) { s.ReportID = "ops.none" }), report.ErrNotFound},
		{"no owner", mod(func(s *report.Schedule) { s.Owner = "" }), report.ErrInvalid},
		{"zero every", mod(func(s *report.Schedule) { s.Every = 0 }), report.ErrInvalid},
		{"no anchor", mod(func(s *report.Schedule) { s.Anchor = time.Time{} }), report.ErrInvalid},
		{"params not an object", mod(func(s *report.Schedule) { s.Params = json.RawMessage(`[]`) }), report.ErrInvalidParams},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, s := newScheduler(t)
			err := s.AddSchedule(context.Background(), nil, tt.sc)
			if !errors.Is(err, tt.want) {
				t.Fatalf("AddSchedule: %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSlotStart(t *testing.T) {
	sc := report.Schedule{ID: "s", Every: 24 * time.Hour, Anchor: t0}
	tests := []struct {
		name string
		at   time.Time
		want time.Time
		err  error
	}{
		{"at anchor", t0, t0, nil},
		{"inside first slot", t0.Add(23 * time.Hour), t0, nil},
		{"next slot", t0.Add(25 * time.Hour), t0.Add(24 * time.Hour), nil},
		{"before anchor", t0.Add(-time.Second), time.Time{}, report.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := report.SlotStart(sc, tt.at)
			if !errors.Is(err, tt.err) || !got.Equal(tt.want) {
				t.Fatalf("SlotStart = %v, %v; want %v, %v", got, err, tt.want, tt.err)
			}
		})
	}
}

// A rerun of the same schedule slot creates no second run.
func TestRunSlotOncePerSlot(t *testing.T) {
	tests := []struct {
		name     string
		at       []time.Time
		wantRuns int
		created  []bool
	}{
		{"same instant twice", []time.Time{t0.Add(time.Hour), t0.Add(time.Hour)}, 1, []bool{true, false}},
		{"two times in one slot", []time.Time{t0.Add(time.Hour), t0.Add(20 * time.Hour)}, 1, []bool{true, false}},
		{"two slots", []time.Time{t0.Add(time.Hour), t0.Add(25 * time.Hour)}, 2, []bool{true, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, s := newScheduler(t)
			var first report.Run
			for i, at := range tt.at {
				r, created, err := s.RunSlot(context.Background(), nil, "sch-1", at)
				if err != nil {
					t.Fatal(err)
				}
				if created != tt.created[i] {
					t.Fatalf("call %d: created = %v, want %v", i, created, tt.created[i])
				}
				if r.ScheduleID != "sch-1" || r.By != "" {
					t.Fatalf("call %d: run %+v: want schedule sch-1 and no user", i, r)
				}
				if i == 0 {
					first = r
				} else if !created && r.ID != first.ID {
					t.Fatalf("rerun returned run %s, want %s", r.ID, first.ID)
				}
			}
			runs, err := e.Runs(context.Background(), "ops.aging")
			if err != nil {
				t.Fatal(err)
			}
			if len(runs) != tt.wantRuns {
				t.Fatalf("runs = %d, want %d", len(runs), tt.wantRuns)
			}
		})
	}
}

func TestRunSlotErrors(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		at       time.Time
		want     error
	}{
		{"unknown schedule", "sch-x", t0, report.ErrNotFound},
		{"before anchor", "sch-1", t0.Add(-time.Hour), report.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, s := newScheduler(t)
			if _, _, err := s.RunSlot(context.Background(), nil, tt.schedule, tt.at); !errors.Is(err, tt.want) {
				t.Fatalf("RunSlot: %v, want %v", err, tt.want)
			}
		})
	}
}

func TestOwnerWithoutCapabilityCannotRunSlot(t *testing.T) {
	e, _ := newEngine(t)
	s := e
	sc := report.Schedule{ID: "sch-b", ReportID: "ops.aging", Params: json.RawMessage(`{"period":"2026-09"}`), Every: time.Hour, Anchor: t0, Owner: "bob"}
	if err := s.AddSchedule(context.Background(), nil, sc); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RunSlot(context.Background(), nil, "sch-b", t0); !errors.Is(err, report.ErrForbidden) {
		t.Fatalf("RunSlot: %v, want ErrForbidden", err)
	}
}

// A scheduled run is a River job: unique by args, and a redelivered job creates no second run.
func TestSlotJobIsRiverJob(t *testing.T) {
	var args river.JobArgs = report.SlotArgs{}
	if args.Kind() != report.JobKindSlot {
		t.Fatalf("kind = %q", args.Kind())
	}
	if opts := (report.SlotArgs{}).InsertOpts(); !opts.UniqueOpts.ByArgs {
		t.Fatal("slot job must be unique by args")
	}
	var _ river.Worker[report.SlotArgs] = (*report.SlotWorker)(nil)

	e, s := newScheduler(t)
	txs := 0
	w := &report.SlotWorker{Reports: s, InTx: func(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
		txs++
		return fn(ctx, nil)
	}}
	job := &river.Job[report.SlotArgs]{Args: report.SlotArgs{ScheduleID: "sch-1", Slot: t0}}
	for i := 0; i < 2; i++ {
		if err := w.Work(context.Background(), job); err != nil {
			t.Fatalf("work %d: %v", i, err)
		}
	}
	runs, _ := e.Runs(context.Background(), "ops.aging")
	if len(runs) != 1 || txs != 2 {
		t.Fatalf("runs = %d, txs = %d; want 1 run over 2 deliveries", len(runs), txs)
	}
	if !runs[0].Slot.Equal(t0) {
		t.Fatalf("slot = %v, want %v", runs[0].Slot, t0)
	}
	bad := &river.Job[report.SlotArgs]{Args: report.SlotArgs{ScheduleID: "sch-x", Slot: t0}}
	if err := w.Work(context.Background(), bad); !errors.Is(err, report.ErrNotFound) {
		t.Fatalf("work unknown schedule: %v", err)
	}
	if report.PeriodicJob(report.Schedule{ID: "sch-1", Every: time.Hour, Anchor: t0}, func() time.Time { return t0 }) == nil {
		t.Fatal("PeriodicJob returned nil")
	}
}
