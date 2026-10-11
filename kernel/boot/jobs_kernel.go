package boot

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/kernel/outbox"
)

// Kernel job kinds.
const (
	KindGateRunDue  = "gate.run_due"
	KindOutboxRelay = "outbox.relay"
)

const (
	// gateRunDueEvery is how often gates past their TTL are expired and cooled-off effects run.
	gateRunDueEvery = time.Minute
	// outboxRelayEvery is how often the relay drains pending outbox messages.
	outboxRelayEvery = 5 * time.Second
	// outboxRelayBatch is the most messages one relay run handles.
	outboxRelayBatch = 100
)

type gateRunDueArgs struct{}

func (gateRunDueArgs) Kind() string { return KindGateRunDue }

type outboxRelayArgs struct{}

func (outboxRelayArgs) Kind() string { return KindOutboxRelay }

// gateRunDueWorker runs Gates.RunDue in one transaction. The effect semantics (expiry, effect
// once, cooling-off) belong to the gate engine; this worker only schedules them.
type gateRunDueWorker struct {
	river.WorkerDefaults[gateRunDueArgs]
	k *Kernel
}

func (w gateRunDueWorker) Work(ctx context.Context, _ *river.Job[gateRunDueArgs]) error {
	err := w.k.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := w.k.Gates.RunDue(ctx, tx)
		return err
	})
	if err != nil {
		return fmt.Errorf("%s: %w", KindGateRunDue, err)
	}
	return nil
}

// outboxRelayWorker hands pending outbox messages to every registered consumer.
type outboxRelayWorker struct {
	river.WorkerDefaults[outboxRelayArgs]
	k *Kernel
}

func (w outboxRelayWorker) Work(ctx context.Context, _ *river.Job[outboxRelayArgs]) error {
	// rule: ARC-I-030
	// Consumers claim (consumer, message id) in their effect's transaction, so a redelivery
	// after a crash is a no-op.
	if _, err := w.k.Outbox.Deliver(ctx, outboxRelayBatch); err != nil {
		return fmt.Errorf("%s: %w", KindOutboxRelay, err)
	}
	return nil
}

// AddConsumer adds an outbox consumer to the relay. Consumer names are inbox keys, so a
// name must be non-empty and unique. It fails when the kernel has no outbox.
func (k *Kernel) AddConsumer(c outbox.Consumer) error {
	if k.Outbox == nil {
		return fmt.Errorf("add outbox consumer %q: no outbox: %w", c.Name, ErrConfig)
	}
	if c.Name == "" || c.Handle == nil {
		return fmt.Errorf("add outbox consumer %q: name and handler required: %w", c.Name, ErrConfig)
	}
	for _, have := range k.Outbox.Consumers {
		if have.Name == c.Name {
			return fmt.Errorf("add outbox consumer %q: duplicate name: %w", c.Name, ErrConfig)
		}
	}
	k.Outbox.Consumers = append(k.Outbox.Consumers, c)
	return nil
}

// periodicEvery returns a periodic job that enqueues args every d, at most one per period.
func periodicEvery(d time.Duration, args river.JobArgs) *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(d), func() (river.JobArgs, *river.InsertOpts) {
		return args, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByPeriod: d}}
	}, nil)
}

// addKernelJobs adds the kernel's own workers and periodic jobs: gate.run_due when the kernel
// has a gate engine and a DB, and outbox.relay when it has an outbox. A missing dependency
// leaves its kind out; it is not an error.
func addKernelJobs(k *Kernel) error {
	if k == nil || k.Jobs == nil {
		return fmt.Errorf("add kernel jobs: no job set: %w", ErrConfig)
	}
	if k.Gates != nil && k.DB != nil {
		if err := AddWorker[gateRunDueArgs](k.Jobs, gateRunDueWorker{k: k}); err != nil {
			return fmt.Errorf("add kernel jobs: %w", err)
		}
		k.Jobs.AddPeriodic(periodicEvery(gateRunDueEvery, gateRunDueArgs{}), KindGateRunDue)
	}
	if k.Outbox != nil {
		if k.Outbox.DB == nil && k.DB != nil {
			k.Outbox.DB = k.DB
		}
		if k.Outbox.Relay == nil || k.Outbox.Inbox == nil || k.Outbox.DB == nil {
			return fmt.Errorf("add kernel jobs: outbox needs a relay, an inbox and a DB: %w", ErrConfig)
		}
		// rule: ARC-I-030
		if err := AddWorker[outboxRelayArgs](k.Jobs, outboxRelayWorker{k: k}); err != nil {
			return fmt.Errorf("add kernel jobs: %w", err)
		}
		k.Jobs.AddPeriodic(periodicEvery(outboxRelayEvery, outboxRelayArgs{}), KindOutboxRelay)
	}
	return nil
}
