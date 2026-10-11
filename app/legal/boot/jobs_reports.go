package boot

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/report"
)

// ScheduleSource lists the stored report schedules. Schedules live in the report store, not
// in code, so the set of periodic report jobs is read once at Start.
type ScheduleSource interface {
	Schedules(ctx context.Context) ([]report.Schedule, error)
}

// slotWorker runs one report schedule slot (kind report.slot) on the kernel report engine.
// It has report.SlotWorker's behaviour over the report.Engine contract, since the kernel hands
// apps an Engine and report.SlotWorker needs the concrete *report.Reports.
type slotWorker struct {
	river.WorkerDefaults[report.SlotArgs]
	k *kboot.Kernel
}

// Work runs the slot in one transaction. A slot that already ran (RunSlot created false) is a
// success and runs nothing, so a retried or duplicated job creates no second run.
func (w slotWorker) Work(ctx context.Context, job *river.Job[report.SlotArgs]) error {
	err := w.k.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, _, err := w.k.Reports.RunSlot(ctx, tx, job.Args.ScheduleID, job.Args.Slot)
		return err
	})
	if err != nil {
		return fmt.Errorf("%s %q: %w", report.JobKindSlot, job.Args.ScheduleID, err)
	}
	return nil
}

// addReportWorker adds the one worker for kind report.slot. It is added with or without
// schedules, so schedules created later have a worker.
func addReportWorker(k *kboot.Kernel) error {
	if k == nil || k.Jobs == nil {
		return fmt.Errorf("report jobs: kernel job set: %w", ErrJobDependency)
	}
	if err := requireDeps("report jobs", []dep{
		{"kernel Reports", k.Reports == nil},
		{"kernel DB", k.DB == nil},
	}); err != nil {
		return err
	}
	if err := kboot.AddWorker[report.SlotArgs](k.Jobs, slotWorker{k: k}); err != nil {
		return fmt.Errorf("report jobs: %w", err)
	}
	return nil
}

// startReportSchedules loads every stored schedule into the report engine, in one
// transaction, and adds one report.PeriodicJob per schedule. addReportWorker must have run.
func startReportSchedules(ctx context.Context, k *kboot.Kernel, src ScheduleSource) error {
	if k == nil || k.Jobs == nil {
		return fmt.Errorf("report schedules: kernel job set: %w", ErrJobDependency)
	}
	if err := requireDeps("report schedules", []dep{
		{"ScheduleSource", src == nil},
		{"kernel Reports", k.Reports == nil},
		{"kernel DB", k.DB == nil},
		{"kernel Clock", k.Clock == nil},
	}); err != nil {
		return err
	}
	scheds, err := src.Schedules(ctx)
	if err != nil {
		return fmt.Errorf("report schedules: load: %w", err)
	}
	err = k.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for _, sc := range scheds {
			if err := k.Reports.AddSchedule(ctx, tx, sc); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("report schedules: %w", err)
	}
	for _, sc := range scheds {
		k.Jobs.AddPeriodic(report.PeriodicJob(sc, k.Clock.Now), report.JobKindSlot)
	}
	return nil
}
