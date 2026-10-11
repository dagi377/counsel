// Package migrate runs the parts of a schema change that do not belong in a migration file:
// batched resumable backfills (MIG-062) and cell rollout waves (MIG-064, MIG-065).
package migrate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

// Limits for one backfill. The batch cap is the strictest of the sources (ARC-U-018 1,000
// rows per transaction; MIG-062 5,000; the task 10,000).
const (
	MaxBatchSize     = 1000
	DefaultPause     = 200 * time.Millisecond
	DefaultMaxLag    = 5 * time.Second
	DefaultLagPoll   = time.Second
	DefaultBatchTime = time.Second
)

// Sentinel errors.
var (
	ErrBatchTooLarge = errors.New("migrate: batch size above the per-transaction cap")
	ErrBadOptions    = errors.New("migrate: invalid backfill options")
)

// Step is one backfill. FillBatch fills, in one committed transaction, up to limit rows whose
// primary key is after the cursor and whose new value is still NULL (WHERE new_col IS NULL, so
// a repeated batch is harmless). It returns the cursor of the last row it read in key order and
// the number of rows it read; zero rows means the step is done. The cursor is opaque to the
// runner: the store encodes the primary key and the empty cursor means "from the start".
type Step interface {
	Name() string
	FillBatch(ctx context.Context, after string, limit int) (last string, rows int, err error)
}

// Checkpoint is the progress of one step, saved after every committed batch.
type Checkpoint struct {
	After   string // cursor of the last committed batch
	Batches int    // committed batches so far
	Rows    int64  // rows read so far
	Done    bool
}

// Checkpoints stores progress by step name. Load returns ok=false when the step never ran.
type Checkpoints interface {
	Load(ctx context.Context, step string) (cp Checkpoint, ok bool, err error)
	Save(ctx context.Context, step string, cp Checkpoint) error
}

// LagSource reports how far the replicas are behind the primary.
type LagSource interface {
	ReplicationLag(ctx context.Context) (time.Duration, error)
}

// Sleeper waits for d or until ctx is done. Tests inject one that only advances a fake clock.
type Sleeper interface {
	Sleep(ctx context.Context, d time.Duration) error
}

// Options configure one Backfill run. Zero durations take the defaults above.
type Options struct {
	BatchSize   int           // rows per transaction, 1..MaxBatchSize (default MaxBatchSize)
	Pause       time.Duration // sleep between batches
	MaxLag      time.Duration // pause while replication lag is above this
	LagPoll     time.Duration // how often to re-check lag while paused
	BatchTime   time.Duration // a slower batch halves the next batch size
	Checkpoints Checkpoints   // required
	Lag         LagSource     // optional; nil means no replicas to wait for
	Clock       clock.Clock   // required: times each batch
	Sleep       Sleeper       // required
}

// Result summarises one run.
type Result struct {
	StartBatch int   // committed batches before this run (non-zero when resumed)
	Batches    int   // committed batches in total, including earlier runs
	Rows       int64 // rows read in total, including earlier runs
	LagPauses  int   // lag checks that found the replicas too far behind
}

func (o *Options) normalize() error {
	if o.BatchSize == 0 {
		o.BatchSize = MaxBatchSize
	}
	// rule: MIG-062
	// rule: ARC-U-018
	if o.BatchSize > MaxBatchSize {
		return fmt.Errorf("%w: %d > %d", ErrBatchTooLarge, o.BatchSize, MaxBatchSize)
	}
	if o.BatchSize < 0 || o.Pause < 0 || o.MaxLag < 0 || o.LagPoll < 0 || o.BatchTime < 0 {
		return fmt.Errorf("%w: negative value", ErrBadOptions)
	}
	if o.Checkpoints == nil || o.Clock == nil || o.Sleep == nil {
		return fmt.Errorf("%w: Checkpoints, Clock and Sleep are required", ErrBadOptions)
	}
	if o.Pause == 0 {
		o.Pause = DefaultPause
	}
	if o.MaxLag == 0 {
		o.MaxLag = DefaultMaxLag
	}
	if o.LagPoll == 0 {
		o.LagPoll = DefaultLagPoll
	}
	if o.BatchTime == 0 {
		o.BatchTime = DefaultBatchTime
	}
	return nil
}

// Backfill runs step to completion: keyset-paginated by primary key, one committed batch of at
// most opts.BatchSize rows at a time, a checkpoint after every commit, a pause between batches,
// and no batch while replication lag is above opts.MaxLag. A run that stops (error, cancel,
// kill) resumes from the last checkpoint.
//
// rule: MIG-062
// rule: ARC-U-018
func Backfill(ctx context.Context, step Step, opts Options) (Result, error) {
	if err := opts.normalize(); err != nil {
		return Result{}, err
	}
	name := step.Name()
	cp, _, err := opts.Checkpoints.Load(ctx, name)
	if err != nil {
		return Result{}, fmt.Errorf("backfill %s: load checkpoint: %w", name, err)
	}
	res := Result{StartBatch: cp.Batches, Batches: cp.Batches, Rows: cp.Rows}
	if cp.Done {
		return res, nil
	}
	if cp.Batches > 0 {
		slog.Info("backfill resumed", "step", name, "batch", cp.Batches)
	}
	size := opts.BatchSize
	for {
		if err := ctx.Err(); err != nil {
			return res, fmt.Errorf("backfill %s: %w", name, err)
		}
		pauses, err := waitForReplicas(ctx, opts)
		res.LagPauses += pauses
		if err != nil {
			return res, fmt.Errorf("backfill %s: %w", name, err)
		}
		start := opts.Clock.Now()
		last, rows, err := step.FillBatch(ctx, cp.After, size)
		if err != nil {
			return res, fmt.Errorf("backfill %s: batch %d: %w", name, cp.Batches+1, err)
		}
		if rows == 0 {
			cp.Done = true
			if err := opts.Checkpoints.Save(ctx, name, cp); err != nil {
				return res, fmt.Errorf("backfill %s: save checkpoint: %w", name, err)
			}
			slog.Info("backfill done", "step", name, "batches", cp.Batches, "rows", cp.Rows)
			return res, nil
		}
		if rows > size {
			return res, fmt.Errorf("backfill %s: batch %d read %d rows, limit %d: %w", name, cp.Batches+1, rows, size, ErrBatchTooLarge)
		}
		cp.After, cp.Batches, cp.Rows = last, cp.Batches+1, cp.Rows+int64(rows)
		if err := opts.Checkpoints.Save(ctx, name, cp); err != nil {
			return res, fmt.Errorf("backfill %s: save checkpoint: %w", name, err)
		}
		res.Batches, res.Rows = cp.Batches, cp.Rows
		// A slow batch holds locks too long: halve the next one.
		if opts.Clock.Now().Sub(start) > opts.BatchTime && size > 1 {
			size /= 2
		}
		if err := opts.Sleep.Sleep(ctx, opts.Pause); err != nil {
			return res, fmt.Errorf("backfill %s: %w", name, err)
		}
	}
}

// waitForReplicas blocks while replication lag is above opts.MaxLag and returns how many
// checks found it too high.
func waitForReplicas(ctx context.Context, opts Options) (int, error) {
	if opts.Lag == nil {
		return 0, nil
	}
	pauses := 0
	for {
		lag, err := opts.Lag.ReplicationLag(ctx)
		if err != nil {
			return pauses, fmt.Errorf("replication lag: %w", err)
		}
		if lag <= opts.MaxLag {
			return pauses, nil
		}
		pauses++
		if err := opts.Sleep.Sleep(ctx, opts.LagPoll); err != nil {
			return pauses, err
		}
	}
}
