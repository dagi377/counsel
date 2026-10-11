package boot

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
)

const (
	// KindWithdrawalReleaseExpired cancels withdrawal authorisations past their expiry.
	KindWithdrawalReleaseExpired = "trust.withdrawal_release_expired"
	// withdrawalReleaseEvery is how often expired authorisations are released.
	withdrawalReleaseEvery = 15 * time.Minute
)

// expiredReleaser cancels every withdrawal authorisation expired at now (withdraw.PGStore).
type expiredReleaser interface {
	ReleaseExpired(ctx context.Context, tx pgx.Tx, now time.Time) (int64, error)
}

var _ expiredReleaser = withdraw.PGStore{}

type withdrawReleaseArgs struct{}

func (withdrawReleaseArgs) Kind() string { return KindWithdrawalReleaseExpired }

// withdrawReleaseWorker runs one release pass in one transaction. An expired authorisation can
// never execute (PGStore.Lock fails closed), so this only frees the hold it keeps on the
// ledger's available balance.
type withdrawReleaseWorker struct {
	river.WorkerDefaults[withdrawReleaseArgs]
	db    db.DB
	clock clock.Clock
	store expiredReleaser
}

func (w withdrawReleaseWorker) Work(ctx context.Context, _ *river.Job[withdrawReleaseArgs]) error {
	var released int64
	err := w.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		n, err := w.store.ReleaseExpired(ctx, tx, w.clock.Now())
		released = n
		return err
	})
	if err != nil {
		return fmt.Errorf("%s: %w", KindWithdrawalReleaseExpired, err)
	}
	slog.InfoContext(ctx, "expired withdrawal authorisations released", "count", released)
	return nil
}

// addWithdrawJobs adds kind trust.withdrawal_release_expired, run every 15 minutes, when the
// gate services are built (trust withdrawals exist only then). It needs the kernel DB and clock.
func (w *wiring) addWithdrawJobs(_ context.Context, k *kboot.Kernel) error {
	if w.opts.Gates == nil {
		return nil
	}
	return addWithdrawJobs(k, withdraw.PGStore{})
}

func addWithdrawJobs(k *kboot.Kernel, store expiredReleaser) error {
	if k == nil || k.Jobs == nil {
		return fmt.Errorf("withdraw jobs: kernel job set: %w", ErrJobDependency)
	}
	if err := requireDeps("withdraw jobs", []dep{
		{"kernel DB", k.DB == nil},
		{"kernel Clock", k.Clock == nil},
		{"Store", store == nil},
	}); err != nil {
		return err
	}
	wk := withdrawReleaseWorker{db: k.DB, clock: k.Clock, store: store}
	if err := kboot.AddWorker[withdrawReleaseArgs](k.Jobs, wk); err != nil {
		return fmt.Errorf("withdraw jobs: %w", err)
	}
	k.Jobs.AddPeriodic(periodicEvery(withdrawalReleaseEvery, withdrawReleaseArgs{}), KindWithdrawalReleaseExpired)
	return nil
}
