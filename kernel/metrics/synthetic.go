package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// rule: ARC-U-035
// SyntheticInterval is how often each location runs the synthetic journey for a cell.
const SyntheticInterval = 60 * time.Second

// rule: ARC-U-031
// SyntheticStepTimeout bounds one synthetic step like an interactive external call (total ≤ 10 s).
const SyntheticStepTimeout = 10 * time.Second

// Synthetic metric names. Labels are limited to AllowedLabels ("check", "status").
const (
	SyntheticRunsMetric   = "synthetic_check_total"
	SyntheticFailedMetric = "synthetic_check_failed_total"
	SyntheticTookMetric   = "synthetic_check_seconds"
)

// rule: ARC-U-034
// SLIMetrics are the per-cell SLIs every cell exports; deploy/observability alerts on them.
var SLIMetrics = []string{
	"slo_valid_total", "slo_bad_total", "http_request_seconds", "outbox_lag_seconds",
	"job_queue_age_seconds", "dlq_size", "wal_archive_age_seconds", "replication_lag_seconds",
	"reminder_freshness_seconds", "backup_age_seconds",
}

// rule: ARC-U-036
// StatusComponents are the public status page components; every page alert maps to one.
var StatusComponents = []string{"app", "documents", "payments", "e-sign", "email", "agent"}

// rule: ARC-U-037
// IncidentSeverities are the incident levels an alert may declare. A fast burn is SEV2.
var IncidentSeverities = []string{"SEV1", "SEV2", "SEV3"}

// SyntheticStep is one step of the synthetic journey, registered by the app (open a list,
// create and delete a 0.1 h entry in the flagged synthetic record, document round-trip).
type SyntheticStep struct {
	Name string
	Run  CheckFunc
}

// SyntheticUser is the flagged synthetic account the journey logs in with.
type SyntheticUser struct {
	Login  string
	Secret string
}

// rule: ARC-U-040
// LogValue keeps the secret out of every log line.
func (u SyntheticUser) LogValue() slog.Value {
	return slog.GroupValue(slog.String("login", u.Login), slog.String("secret", "[redacted]"))
}

// String keeps the secret out of fmt output and error messages.
func (u SyntheticUser) String() string { return fmt.Sprintf("synthetic user %s", u.Login) }

// LoginFunc signs the synthetic user in.
type LoginFunc func(ctx context.Context, u SyntheticUser) error

// Synthetic runs the synthetic journey for one cell from one location.
type Synthetic struct {
	Location string
	User     SyntheticUser
	Login    LoginFunc
	Steps    []SyntheticStep
	Recorder Recorder
	// StepTimeout overrides SyntheticStepTimeout when > 0 (tests).
	StepTimeout time.Duration
}

// loginCheck is the result name of the login step.
const loginCheck = "login"

// RunOnce logs in, then runs every step in order. A failed login skips the steps, since
// they cannot pass signed out. Results carry no error text.
// rule: ARC-U-035
func (s *Synthetic) RunOnce(ctx context.Context) []CheckResult {
	res := []CheckResult{s.step(ctx, loginCheck, func(ctx context.Context) error { return s.Login(ctx, s.User) })}
	if !res[0].OK {
		return res
	}
	for _, st := range s.Steps {
		res = append(res, s.step(ctx, st.Name, st.Run))
	}
	return res
}

// Run calls RunOnce on every tick until ctx is done. Production passes a
// time.NewTicker(SyntheticInterval).C; tests pass their own channel. A run that has started
// finishes (each step is still bounded by its step timeout), so no step outlives Run.
func (s *Synthetic) Run(ctx context.Context, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			s.RunOnce(context.WithoutCancel(ctx))
		}
	}
}

func (s *Synthetic) step(ctx context.Context, name string, fn CheckFunc) CheckResult {
	timeout := s.StepTimeout
	if timeout <= 0 {
		timeout = SyntheticStepTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- fn(cctx) }()
	var err error
	// rule: ARC-U-031
	select {
	case err = <-done:
	case <-cctx.Done():
		err = cctx.Err()
	}
	r := CheckResult{Name: name, OK: err == nil, Took: time.Since(start)}
	status := "ok"
	if !r.OK {
		status = "failed"
		// rule: ARC-U-040
		// The error may echo credentials or record data; only the step name is logged.
		slog.WarnContext(ctx, "synthetic check failed", "check", name, "location", s.Location)
	}
	s.record(ctx, r, status)
	return r
}

func (s *Synthetic) record(ctx context.Context, r CheckResult, status string) {
	if s.Recorder == nil {
		return
	}
	labels := map[string]string{"check": r.Name, "status": status}
	if err := s.Recorder.Inc(ctx, SyntheticRunsMetric, labels); err != nil {
		slog.WarnContext(ctx, "synthetic metric", "check", r.Name, "err", err)
	}
	if !r.OK {
		if err := s.Recorder.Inc(ctx, SyntheticFailedMetric, map[string]string{"check": r.Name}); err != nil {
			slog.WarnContext(ctx, "synthetic metric", "check", r.Name, "err", err)
		}
	}
	if err := s.Recorder.Observe(ctx, SyntheticTookMetric, r.Took, map[string]string{"check": r.Name}); err != nil {
		slog.WarnContext(ctx, "synthetic metric", "check", r.Name, "err", err)
	}
}
