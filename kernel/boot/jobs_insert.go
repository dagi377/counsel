package boot

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/dagi377/counsel/kernel/db"
)

// JobInserter inserts River jobs without working them. *river.Client satisfies it. Apps
// read it from Kernel.Enqueuer to queue jobs that the worker role runs.
type JobInserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// openInserter opens the insert-only job client and returns it with the function that
// closes its pool.
type openInserter func(ctx context.Context, cfg db.Config) (JobInserter, func(), error)

// pgInserter is the production openInserter: an insert-only River client over the direct
// pool. It has no workers and no queues, so it never starts a producer and works no job;
// the worker role runs what it inserts.
func pgInserter(ctx context.Context, cfg db.Config) (JobInserter, func(), error) {
	// rule: ARC-U-011
	// The job queue connects directly, never through the transaction-mode pooler.
	pool, err := db.OpenDirect(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("job inserter: %w", err)
	}
	c, err := newInsertClient(pool)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return c, pool.Close, nil
}

// newInsertClient is a River client that only inserts: no Workers, no Queues.
func newInsertClient(pool *pgxpool.Pool) (JobInserter, error) {
	c, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		return nil, fmt.Errorf("job inserter: river client: %w", err)
	}
	return c, nil
}
