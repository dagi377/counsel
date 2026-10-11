package boot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river"

	cliosync "github.com/dagi377/counsel/app/legal/clio/sync"
	"github.com/dagi377/counsel/app/legal/legalflags"
	kboot "github.com/dagi377/counsel/kernel/boot"
)

// ErrJobDependency is returned when an app job is added without a dependency it needs. The
// error text names the dependency.
var ErrJobDependency = errors.New("legal boot: job dependency missing")

// FlagSourceSync gates the source import/sync jobs. app/legal/flags.go defines it, default off.
const FlagSourceSync = legalflags.ImportSourceSync

// RenewPorts are the subscription renewal job's dependencies that other modules own.
type RenewPorts struct {
	// Store reads and updates platform.source_subscription.
	Store cliosync.SubscriptionStore
	// Renewer extends a subscription at the source.
	Renewer cliosync.SubscriptionRenewer
	// Notifier emits renewal-failed events and alerts.
	Notifier cliosync.RenewalNotifier
}

type renewArgs struct{}

func (renewArgs) Kind() string { return cliosync.RenewJobKind }

// renewWorker runs one renewal pass. A failed renewal is recorded on its row by RenewJob and
// retried by the next hourly run, not by River's retry.
type renewWorker struct {
	river.WorkerDefaults[renewArgs]
	job *cliosync.RenewJob
}

func (w renewWorker) Work(ctx context.Context, _ *river.Job[renewArgs]) error {
	if _, err := w.job.Run(ctx); err != nil {
		return fmt.Errorf("%s: %w", cliosync.RenewJobKind, err)
	}
	return nil
}

// newRenewWorker wraps a cliosync.RenewJob built from the kernel clock and p.
func newRenewWorker(k *kboot.Kernel, p RenewPorts) renewWorker {
	return renewWorker{job: &cliosync.RenewJob{Store: p.Store, Source: p.Renewer, Notifier: p.Notifier, Clock: k.Clock}}
}

// addRenewJobs adds kind subscription.renew, run hourly, when FlagSourceSync is on. With the
// flag off it adds nothing and needs no ports.
func addRenewJobs(k *kboot.Kernel, p RenewPorts) error {
	if k == nil || k.Jobs == nil {
		return fmt.Errorf("renew jobs: kernel job set: %w", ErrJobDependency)
	}
	if k.Flags == nil || !k.Flags.Enabled(FlagSourceSync) {
		return nil
	}
	if err := requireDeps("renew jobs", []dep{
		{"kernel Clock", k.Clock == nil},
		{"Store", p.Store == nil},
		{"Renewer", p.Renewer == nil},
		{"Notifier", p.Notifier == nil},
	}); err != nil {
		return err
	}
	if err := kboot.AddWorker[renewArgs](k.Jobs, newRenewWorker(k, p)); err != nil {
		return fmt.Errorf("renew jobs: %w", err)
	}
	k.Jobs.AddPeriodic(periodicEvery(cliosync.RenewInterval, renewArgs{}), cliosync.RenewJobKind)
	return nil
}

// dep is one named dependency and whether it is missing.
type dep struct {
	name    string
	missing bool
}

// requireDeps fails naming the first missing dependency.
func requireDeps(what string, deps []dep) error {
	for _, d := range deps {
		if d.missing {
			return fmt.Errorf("%s: %s: %w", what, d.name, ErrJobDependency)
		}
	}
	return nil
}

// periodicEvery returns a periodic job that enqueues args every d, at most one per period, so
// two workers running at once enqueue one job.
func periodicEvery(d time.Duration, args river.JobArgs) *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(d), func() (river.JobArgs, *river.InsertOpts) {
		return args, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByPeriod: d}}
	}, nil)
}
