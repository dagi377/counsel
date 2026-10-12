package boot

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/platform"
	"github.com/dagi377/counsel/kernel/audit"
	kboot "github.com/dagi377/counsel/kernel/boot"
)

const (
	// releaseSendEvery is how often queued releases are sent.
	releaseSendEvery = time.Minute
	// destructionRunEvery is how often scheduled destruction orders are executed.
	destructionRunEvery = time.Hour
)

type releaseSendArgs struct{}

func (releaseSendArgs) Kind() string { return platform.KindReleaseSend }

// releaseSendWorker runs one ReleaseRunner pass. Each item commits on its own, so a River
// retry of a failed pass resends nothing already sent.
type releaseSendWorker struct {
	river.WorkerDefaults[releaseSendArgs]
	run platform.ReleaseRunner
}

func (w releaseSendWorker) Work(ctx context.Context, _ *river.Job[releaseSendArgs]) error {
	n, err := w.run.Run(ctx)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "releases sent", "count", n)
	return nil
}

type destructionRunArgs struct{}

func (destructionRunArgs) Kind() string { return platform.KindDestructionRun }

// destructionRunWorker runs one DestructionRunner pass; holds and retention are re-checked
// for every order at run time.
type destructionRunWorker struct {
	river.WorkerDefaults[destructionRunArgs]
	run platform.DestructionRunner
}

func (w destructionRunWorker) Work(ctx context.Context, _ *river.Job[destructionRunArgs]) error {
	res, err := w.run.Run(ctx)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "destruction orders run", "done", res.Done, "blocked", res.Blocked)
	return nil
}

// PlatformJobPorts are the runner dependencies no kernel service supplies.
type PlatformJobPorts struct {
	// Shredder crypto-shreds a matter's file contents outside its keep-set. Nil leaves every
	// eligible order scheduled (the pass fails with platform.ErrNoShredder); holds still block.
	Shredder platform.Shredder
}

// addPlatformJobs adds kinds platform.destruction_run and, when the kernel has an outbox
// emitter (production always does), platform.release_send. It runs only when the gate services
// are built: the platform.* effects that queue their rows exist only then.
func (w *wiring) addPlatformJobs(_ context.Context, k *kboot.Kernel) error {
	if w.opts.Gates == nil {
		return nil
	}
	var p PlatformJobPorts
	if w.opts.Platform != nil {
		p = *w.opts.Platform
	}
	return addPlatformJobs(k, p)
}

func addPlatformJobs(k *kboot.Kernel, p PlatformJobPorts) error {
	if k == nil || k.Jobs == nil {
		return fmt.Errorf("platform jobs: kernel job set: %w", ErrJobDependency)
	}
	if err := requireDeps("platform jobs", []dep{
		{"kernel DB", k.DB == nil},
		{"kernel Clock", k.Clock == nil},
	}); err != nil {
		return err
	}
	// The runners only Append, in their own tx; Verify (which reads outside a tx) is not used.
	var q audit.Querier
	if r, ok := k.DB.(readDB); ok {
		q = r
	}
	log := audit.New(q, AuditMasker(), audit.Options{})
	destroy := destructionRunWorker{run: platform.DestructionRunner{DB: k.DB, Store: platform.PGDestructions{}, Records: retentionRecords{},
		Shredder: p.Shredder, Audit: log, Now: k.Clock.Now, Zone: k.Clock.Location()}}
	if err := kboot.AddWorker[destructionRunArgs](k.Jobs, destroy); err != nil {
		return fmt.Errorf("platform jobs: %w", err)
	}
	k.Jobs.AddPeriodic(periodicEvery(destructionRunEvery, destructionRunArgs{}), platform.KindDestructionRun)
	if k.Emitter == nil {
		slog.Warn("no outbox emitter: approved releases stay queued", "kind", platform.KindReleaseSend)
		return nil
	}
	send := releaseSendWorker{run: platform.ReleaseRunner{DB: k.DB, Store: platform.PGReleases{},
		Sender: platform.EventSender{Emitter: k.Emitter}, Audit: log}}
	if err := kboot.AddWorker[releaseSendArgs](k.Jobs, send); err != nil {
		return fmt.Errorf("platform jobs: %w", err)
	}
	k.Jobs.AddPeriodic(periodicEvery(releaseSendEvery, releaseSendArgs{}), platform.KindReleaseSend)
	return nil
}

// retentionRecords adapts the matter module's retention record to the platform runner.
type retentionRecords struct{ pg matter.PGRetention }

var _ platform.RetentionRecords = retentionRecords{}

func (r retentionRecords) Retention(ctx context.Context, tx pgx.Tx, matterID string) (platform.MatterRetention, error) {
	m, err := r.pg.Retention(ctx, tx, matterID)
	if err != nil {
		return platform.MatterRetention{}, err
	}
	return platform.MatterRetention{Found: m.Found, Status: m.Status, MatterHold: m.MatterHold, RecordHold: m.RecordHold,
		DocumentHold: m.DocumentHold, RelatedOpen: m.RelatedOpen, RetentionClass: m.RetentionClass,
		DestroyAfter: m.DestroyAfter, AlreadyDestroyed: m.AlreadyDestroyed, KeepSet: m.KeepSet}, nil
}

func (r retentionRecords) RecordDestroyed(ctx context.Context, tx pgx.Tx, matterID, gateID string, at time.Time) error {
	return r.pg.RecordDestroyed(ctx, tx, matterID, gateID, at)
}
