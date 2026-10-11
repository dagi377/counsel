package report

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// JobKindSlot is the River job kind of one scheduled report slot.
const JobKindSlot = "report.slot"

// SlotArgs is the River job for one slot of a schedule. River keeps one job per args, and
// RunSlot runs a slot at most once, so a rerun of the same slot creates no second run.
type SlotArgs struct {
	ScheduleID string    `json:"schedule_id"`
	Slot       time.Time `json:"slot"`
}

// Kind implements river.JobArgs.
func (SlotArgs) Kind() string { return JobKindSlot }

// InsertOpts makes the job unique by its args: one job per schedule slot.
func (SlotArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// Scheduler stores schedules and runs each slot of a schedule through the report engine at
// most once. A scheduled run uses the schedule owner's capabilities and records no user.
type Scheduler struct {
	reports *Reports

	mu        sync.Mutex
	schedules map[string]Schedule
	slots     map[slotKey]RunID
}

type slotKey struct {
	schedule string
	slot     int64 // slot start, unix nanoseconds
}

// NewScheduler returns a scheduler with no schedules that runs reports through r.
func NewScheduler(r *Reports) *Scheduler {
	return &Scheduler{reports: r, schedules: map[string]Schedule{}, slots: map[slotKey]RunID{}}
}

// AddSchedule stores s. The report must be registered, Every positive, Owner set and Params
// a JSON object.
func (s *Scheduler) AddSchedule(_ context.Context, _ pgx.Tx, sc Schedule) error {
	if sc.ID == "" || sc.Owner == "" || sc.Every <= 0 || sc.Anchor.IsZero() {
		return fmt.Errorf("add schedule %q: %w", sc.ID, ErrInvalid)
	}
	if _, ok := s.reports.lookup(sc.ReportID); !ok {
		return fmt.Errorf("add schedule %q: report %q: %w", sc.ID, sc.ReportID, ErrNotFound)
	}
	if _, err := ParamsHash(sc.Params); err != nil {
		return fmt.Errorf("add schedule %q: %w", sc.ID, err)
	}
	sc.Params = append(json.RawMessage(nil), sc.Params...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.schedules[sc.ID]; ok {
		return fmt.Errorf("add schedule %q: %w", sc.ID, ErrDuplicate)
	}
	s.schedules[sc.ID] = sc
	return nil
}

// SlotStart returns the start of the slot of sc that contains at. Slots start at multiples
// of Every from Anchor; a time before Anchor has no slot.
func SlotStart(sc Schedule, at time.Time) (time.Time, error) {
	if sc.Every <= 0 || at.Before(sc.Anchor) {
		return time.Time{}, fmt.Errorf("slot of %q at %s: %w", sc.ID, at.Format(time.RFC3339), ErrInvalid)
	}
	n := at.Sub(sc.Anchor) / sc.Every
	return sc.Anchor.Add(n * sc.Every), nil
}

// RunSlot runs schedule scheduleID for the slot containing at. A slot that already ran
// returns its run with created false and runs nothing.
func (s *Scheduler) RunSlot(ctx context.Context, tx pgx.Tx, scheduleID string, at time.Time) (Run, bool, error) {
	s.mu.Lock()
	sc, ok := s.schedules[scheduleID]
	s.mu.Unlock()
	if !ok {
		return Run{}, false, fmt.Errorf("run slot %q: %w", scheduleID, ErrNotFound)
	}
	slot, err := SlotStart(sc, at)
	if err != nil {
		return Run{}, false, fmt.Errorf("run slot: %w", err)
	}
	key := slotKey{schedule: scheduleID, slot: slot.UnixNano()}

	// Hold the lock across the run so two workers on the same slot cannot both run it.
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, done := s.slots[key]; done {
		r, err := s.reports.runByID(id)
		if err != nil {
			return Run{}, false, fmt.Errorf("run slot %q: %w", scheduleID, err)
		}
		return r, false, nil
	}
	en, ok := s.reports.lookup(sc.ReportID)
	if !ok {
		return Run{}, false, fmt.Errorf("run slot %q: report %q: %w", scheduleID, sc.ReportID, ErrNotFound)
	}
	r, _, err := s.reports.execute(ctx, tx, en, sc.Params, sc.Owner, "")
	if err != nil {
		return Run{}, false, fmt.Errorf("run slot %q: %w", scheduleID, err)
	}
	r = s.reports.markScheduled(r.ID, scheduleID, slot)
	s.slots[key] = r.ID
	return r, true, nil
}

// runByID returns a recorded run.
func (e *Reports) runByID(id RunID) (Run, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.runs {
		if r.ID == id {
			return r, nil
		}
	}
	return Run{}, fmt.Errorf("run %q: %w", id, ErrNotFound)
}

// markScheduled sets the schedule and slot on a recorded run and returns it.
func (e *Reports) markScheduled(id RunID, scheduleID string, slot time.Time) Run {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.runs {
		if e.runs[i].ID == id {
			e.runs[i].ScheduleID = scheduleID
			e.runs[i].Slot = slot
			return e.runs[i]
		}
	}
	return Run{ID: id, ScheduleID: scheduleID, Slot: slot}
}

// TxFunc runs fn in one database transaction (kernel/db Pool.InTx has this shape).
type TxFunc func(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error

// SlotWorker is the River worker that runs one schedule slot.
type SlotWorker struct {
	river.WorkerDefaults[SlotArgs]
	Scheduler *Scheduler
	InTx      TxFunc
}

// Work implements river.Worker. A slot that already ran is a success, so a retried or
// duplicated job creates no second run.
func (w *SlotWorker) Work(ctx context.Context, job *river.Job[SlotArgs]) error {
	return w.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, _, err := w.Scheduler.RunSlot(ctx, tx, job.Args.ScheduleID, job.Args.Slot); err != nil {
			return fmt.Errorf("report slot job: %w", err)
		}
		return nil
	})
}

// PeriodicJob returns the River periodic job that enqueues sc's current slot every Every.
// now gives the time whose slot is enqueued.
func PeriodicJob(sc Schedule, now func() time.Time) *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(sc.Every), func() (river.JobArgs, *river.InsertOpts) {
		slot, err := SlotStart(sc, now())
		if err != nil {
			// Before the anchor there is no slot yet; nil tells River to skip this tick.
			return nil, nil
		}
		return SlotArgs{ScheduleID: sc.ID, Slot: slot}, nil
	}, &river.PeriodicJobOpts{ID: JobKindSlot + ":" + sc.ID})
}
