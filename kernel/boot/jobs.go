package boot

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/riverqueue/river"
)

// ErrJobs means the job set is inconsistent: a duplicate worker kind or a periodic job
// whose kind has no worker.
var ErrJobs = errors.New("boot: invalid job set")

// Jobs is the River job set the kernel and apps build at boot: one worker per job kind and
// the periodic jobs that enqueue those kinds. The worker role hands Workers and Periodic to
// its River client. Safe for concurrent use.
type Jobs struct {
	Workers *river.Workers

	mu       sync.Mutex
	kinds    map[string]bool
	periodic []periodicJob
}

type periodicJob struct {
	job  *river.PeriodicJob
	kind string
}

// NewJobs returns an empty job set.
func NewJobs() *Jobs {
	return &Jobs{Workers: river.NewWorkers(), kinds: map[string]bool{}}
}

// AddWorker adds the worker for T's kind. A second worker for one kind is an error naming
// the kind; it never panics.
func AddWorker[T river.JobArgs](j *Jobs, w river.Worker[T]) error {
	var args T
	kind := args.Kind()
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.kinds[kind] {
		return fmt.Errorf("add worker %q: duplicate kind: %w", kind, ErrJobs)
	}
	if err := river.AddWorkerSafely(j.Workers, w); err != nil {
		return fmt.Errorf("add worker %q: %w", kind, err)
	}
	j.kinds[kind] = true
	return nil
}

// AddPeriodic adds a periodic job that enqueues jobs of kind. Validate checks that kind has
// a worker.
func (j *Jobs) AddPeriodic(p *river.PeriodicJob, kind string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.periodic = append(j.periodic, periodicJob{job: p, kind: kind})
}

// Periodic returns the periodic jobs in the order they were added.
func (j *Jobs) Periodic() []*river.PeriodicJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]*river.PeriodicJob, len(j.periodic))
	for i, p := range j.periodic {
		out[i] = p.job
	}
	return out
}

// Kinds returns the kinds that have a worker, sorted.
func (j *Jobs) Kinds() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]string, 0, len(j.kinds))
	for k := range j.kinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Validate checks every periodic job has a worker for its kind and a job. An empty set is
// valid. Every problem is returned, joined.
func (j *Jobs) Validate() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	var errs []error
	for i, p := range j.periodic {
		switch {
		case p.job == nil:
			errs = append(errs, fmt.Errorf("periodic job %d (%q): nil job: %w", i, p.kind, ErrJobs))
		case !j.kinds[p.kind]:
			errs = append(errs, fmt.Errorf("periodic job %d: kind %q has no worker: %w", i, p.kind, ErrJobs))
		}
	}
	return errors.Join(errs...)
}
