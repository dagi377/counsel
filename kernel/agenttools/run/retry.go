package run

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/dagi377/counsel/kernel/registry"
)

// JobKindRetry is the River job kind that reruns an agent run a transient model fault ended.
const JobKindRetry = "agent_run_retry"

// EventRunFailed is the event a run emits when its retries are spent.
const EventRunFailed = "run.failed"

// RetryBackoff is the wait before each retry: 1m, 5m, 25m. After the last the run fails.
var RetryBackoff = []time.Duration{time.Minute, 5 * time.Minute, 25 * time.Minute}

// ErrRetryArgs means a retry job is missing its feature id or run id.
var ErrRetryArgs = errors.New("run: retry job needs feature id and run id")

// RetryArgs is the agent_run_retry job. It carries ids only: no prompt, trigger or client
// text (AI-R-005). Attempt is the retry number, from 1.
type RetryArgs struct {
	FeatureID   string   `json:"feature_id" river:"unique"`
	RunID       string   `json:"run_id" river:"unique"`
	TriggerRefs []string `json:"trigger_refs,omitempty"` // trigger source ids (document versions)
	Attempt     int      `json:"attempt" river:"unique"`
}

// Kind implements river.JobArgs.
func (RetryArgs) Kind() string { return JobKindRetry }

// InsertOpts makes the job unique by feature, run id and attempt: queuing the same retry of a
// run twice inserts one job.
func (RetryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// Enqueuer inserts River jobs; *river.Client satisfies it.
type Enqueuer interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// FailureSink records that a run gave up (EventRunFailed) after its retries were spent.
type FailureSink interface {
	RunFailed(ctx context.Context, featureID, runID string, attempts int) error
}

// Resumer rebuilds a run from the ids of a retry job: the feature's spec, the agent
// principal (with the same run id), the prompt and the triggers. Features own the text; the
// job never carries it.
type Resumer interface {
	Resume(ctx context.Context, args RetryArgs) (Spec, Principal, string, []Trigger, error)
}

// Retrier queues retries for runs a transient model fault ended.
type Retrier struct {
	q       Enqueuer
	failed  FailureSink
	backoff []time.Duration
	now     func() time.Time
}

// NewRetrier returns a Retrier over q and failed, with RetryBackoff.
func NewRetrier(q Enqueuer, failed FailureSink) *Retrier {
	return &Retrier{q: q, failed: failed, backoff: slices.Clone(RetryBackoff), now: time.Now}
}

// WithClock sets the clock that schedules retries (tests).
func (r *Retrier) WithClock(now func() time.Time) *Retrier {
	r.now = now
	return r
}

// Queue handles an Outcome. When out.RetryQueued it enqueues retry number done+1 of the run,
// scheduled after the backoff, and reports true. done is the retries already made (0 after
// the first run). When the backoff is spent it records EventRunFailed and reports false. An
// outcome with no RetryQueued does nothing.
func (r *Retrier) Queue(ctx context.Context, spec Spec, p Principal, out Outcome, done int, triggers ...Trigger) (bool, error) {
	if !out.RetryQueued {
		return false, nil
	}
	if spec.FeatureID == "" || p.RunID == "" {
		return false, fmt.Errorf("queue retry: %w", ErrRetryArgs)
	}
	// rule: AI-R-003
	// A spent run fails visibly; nothing retries it again or acts on it.
	if done >= len(r.backoff) {
		if err := r.failed.RunFailed(ctx, spec.FeatureID, p.RunID, done); err != nil {
			return false, fmt.Errorf("run %s: %s: %w", spec.FeatureID, EventRunFailed, err)
		}
		slog.WarnContext(ctx, "agent run failed after retries", "feature", spec.FeatureID, "run", p.RunID, "attempts", done)
		return false, nil
	}
	// rule: AI-R-005
	// The job carries ids only; trigger text stays with the feature.
	args := RetryArgs{FeatureID: spec.FeatureID, RunID: p.RunID, TriggerRefs: triggerRefs(triggers), Attempt: done + 1}
	opts := args.InsertOpts()
	opts.ScheduledAt = r.now().Add(r.backoff[done])
	res, err := r.q.Insert(ctx, args, &opts)
	if err != nil {
		return false, fmt.Errorf("run %s: enqueue retry: %w", spec.FeatureID, err)
	}
	slog.InfoContext(ctx, "agent run retry enqueued", "feature", spec.FeatureID, "run", p.RunID, "attempt", args.Attempt,
		"duplicate", res != nil && res.UniqueSkippedAsDuplicate)
	return true, nil
}

func triggerRefs(triggers []Trigger) []string {
	var refs []string
	for _, t := range triggers {
		if id := t.Source.VersionID; id != "" && !slices.Contains(refs, id) {
			refs = append(refs, id)
		}
	}
	return refs
}

var _ river.Worker[RetryArgs] = (*RetryWorker)(nil)

// RetryWorker runs agent_run_retry jobs: it resumes the run under the same run id, so the
// plan the run reported resumes, and queues the next retry if the model faults again.
type RetryWorker struct {
	river.WorkerDefaults[RetryArgs]
	runner  Runner
	resume  Resumer
	retrier *Retrier
}

// NewRetryWorker returns the worker for JobKindRetry.
func NewRetryWorker(runner Runner, resume Resumer, retrier *Retrier) *RetryWorker {
	return &RetryWorker{runner: runner, resume: resume, retrier: retrier}
}

// Work implements river.Worker.
func (w *RetryWorker) Work(ctx context.Context, job *river.Job[RetryArgs]) error {
	return w.Retry(ctx, job.Args)
}

// Retry reruns one retry job. The resumed principal must keep the job's run id.
func (w *RetryWorker) Retry(ctx context.Context, args RetryArgs) error {
	if args.FeatureID == "" || args.RunID == "" || args.Attempt < 1 {
		return fmt.Errorf("retry: %w", ErrRetryArgs)
	}
	spec, p, prompt, triggers, err := w.resume.Resume(ctx, args)
	if err != nil {
		return fmt.Errorf("retry %s: resume: %w", args.FeatureID, err)
	}
	if spec.FeatureID != args.FeatureID || p.Kind != registry.PrincipalAgent || p.RunID != args.RunID {
		return fmt.Errorf("retry %s: resumed run does not match job: %w", args.FeatureID, ErrRetryArgs)
	}
	out, err := w.runner.Run(ctx, spec, p, prompt, triggers...)
	if err != nil {
		return fmt.Errorf("retry %s: %w", args.FeatureID, err)
	}
	if _, err := w.retrier.Queue(ctx, spec, p, out, args.Attempt, triggers...); err != nil {
		return fmt.Errorf("retry %s: %w", args.FeatureID, err)
	}
	return nil
}
