package boot

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/dagi377/counsel/kernel/db"
)

// Worker role defaults.
const (
	defaultMaxWorkers  = 10
	defaultStopTimeout = 30 * time.Second
)

// WorkerConfig is what the worker role needs beyond the kernel.
type WorkerConfig struct {
	// DB names the Postgres endpoints; the worker uses only the direct one.
	DB db.Config
	// MaxWorkers caps concurrent jobs on the default queue; 0 means 10.
	MaxWorkers int
	// StopTimeout bounds the graceful stop after ctx is cancelled; 0 means 30 s.
	StopTimeout time.Duration
}

// starter is the part of a River client the worker role drives.
type starter interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// newStarter builds the job client over the direct pool.
type newStarter func(pool *pgxpool.Pool, cfg *river.Config) (starter, error)

// newRiverClient is the production newStarter: a River client on the pgx v5 driver.
func newRiverClient(pool *pgxpool.Pool, cfg *river.Config) (starter, error) {
	c, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}
	return c, nil
}

// riverConfig builds the River client config from the job set. It is pure: the workers and
// periodic jobs come from j, and the default queue runs MaxWorkers jobs at once.
func (c WorkerConfig) riverConfig(j *Jobs) *river.Config {
	n := c.MaxWorkers
	if n <= 0 {
		n = defaultMaxWorkers
	}
	return &river.Config{
		Workers:      j.Workers,
		PeriodicJobs: j.Periodic(),
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: n},
		},
	}
}

// runWorker runs the job queue until ctx is cancelled, then stops it gracefully. An invalid
// job set stops the role before any connection is opened or the client is started.
func runWorker(ctx context.Context, k *Kernel, cfg WorkerConfig, build newStarter) error {
	if k == nil || k.Jobs == nil {
		return fmt.Errorf("run worker: no job set: %w", ErrConfig)
	}
	if err := k.Jobs.Validate(); err != nil {
		return fmt.Errorf("run worker: %w", err)
	}
	// rule: ARC-U-011
	// River needs LISTEN/NOTIFY, advisory locks and leader election, so it connects directly,
	// never through the transaction-mode pooler.
	pool, err := db.OpenDirect(ctx, cfg.DB)
	if err != nil {
		return fmt.Errorf("run worker: %w", err)
	}
	defer pool.Close()

	client, err := build(pool, cfg.riverConfig(k.Jobs))
	if err != nil {
		return fmt.Errorf("run worker: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("run worker: start: %w", err)
	}
	slog.InfoContext(ctx, "worker started", "kinds", len(k.Jobs.Kinds()))

	<-ctx.Done()

	timeout := cfg.StopTimeout
	if timeout <= 0 {
		timeout = defaultStopTimeout
	}
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	if err := client.Stop(stopCtx); err != nil {
		return fmt.Errorf("run worker: stop: %w", err)
	}
	slog.InfoContext(stopCtx, "worker stopped")
	return nil
}
