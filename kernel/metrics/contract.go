// CONTRACT: kernel/metrics (T-044). Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Source: spec/eng/a1-uptime.json (SLO-01..10, ARC-U-028 degraded
// mode). Bodies live in the T-044 subtasks (synthetic.go, smoke.go, status.go).

package metrics

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrLabel means a metric label key is not on the allow list; labels never carry
	// customer data.
	ErrLabel = errors.New("metrics: label not allowed")
	// ErrDuplicateCheck means two smoke checks share a name.
	ErrDuplicateCheck = errors.New("metrics: duplicate check")
	// ErrCheckFailed means a smoke check returned an error.
	ErrCheckFailed = errors.New("metrics: check failed")
)

// AllowedLabels are the only metric label keys. No id, name or amount of a customer record.
var AllowedLabels = []string{"route", "method", "status", "module", "job", "dependency", "check", "slo"}

// Recorder records SLO counters and latencies. Implementations reject labels not in
// AllowedLabels with ErrLabel.
type Recorder interface {
	Inc(ctx context.Context, name string, labels map[string]string) error
	Observe(ctx context.Context, name string, d time.Duration, labels map[string]string) error
}

// CheckFunc is one smoke check. It returns nil when the check passes.
type CheckFunc func(ctx context.Context) error

// CheckResult is one check's outcome as the status page shows it. It carries no error text,
// so a failure never leaks customer data.
type CheckResult struct {
	Name string
	OK   bool
	Took time.Duration
}

// Status is the status page body: degraded when any check failed.
type Status struct {
	Degraded bool
	Checks   []CheckResult
}

// Checks holds the registered smoke checks and runs them.
type Checks interface {
	// Add registers a check; a duplicate name returns ErrDuplicateCheck.
	Add(name string, fn CheckFunc) error
	// Run runs every check in name order and reports the status.
	Run(ctx context.Context) Status
}
