package boot

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/app/legal/access"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/identity"
)

const (
	// KindOffboardingCutoff applies approved offboardings whose cut-off has passed (GA12).
	KindOffboardingCutoff = "core.offboarding_cutoff"
	// offboardingCutoffEvery is how often due offboardings are applied.
	offboardingCutoffEvery = 5 * time.Minute
)

type offboardingCutoffArgs struct{}

func (offboardingCutoffArgs) Kind() string { return KindOffboardingCutoff }

// offboardingCutoffWorker applies every due offboarding in one transaction.
type offboardingCutoffWorker struct {
	river.WorkerDefaults[offboardingCutoffArgs]
	k *kboot.Kernel
}

func (w offboardingCutoffWorker) Work(ctx context.Context, _ *river.Job[offboardingCutoffArgs]) error {
	users := identity.UserEvents{Emitter: w.k.Emitter}
	err := w.k.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return access.ApplyDueOffboardings(ctx, tx, w.k.Clock.Now(), users, offboardingHandover)
	})
	if err != nil {
		return fmt.Errorf("%s: %w", KindOffboardingCutoff, err)
	}
	return nil
}

// addOffboardingJobs adds kind core.offboarding_cutoff, run every 5 minutes, in production:
// an approved offboarding applies at its cut-off, not at approval.
func (w *wiring) addOffboardingJobs(_ context.Context, k *kboot.Kernel) error {
	if w.opts.Production == nil {
		return nil
	}
	if k == nil || k.Jobs == nil {
		return fmt.Errorf("offboarding jobs: kernel job set: %w", ErrJobDependency)
	}
	if err := requireDeps("offboarding jobs", []dep{
		{"kernel DB", k.DB == nil},
		{"kernel Clock", k.Clock == nil},
		{"kernel Emitter", k.Emitter == nil},
	}); err != nil {
		return err
	}
	if err := kboot.AddWorker[offboardingCutoffArgs](k.Jobs, offboardingCutoffWorker{k: k}); err != nil {
		return fmt.Errorf("offboarding jobs: %w", err)
	}
	k.Jobs.AddPeriodic(periodicEvery(offboardingCutoffEvery, offboardingCutoffArgs{}), KindOffboardingCutoff)
	return nil
}
