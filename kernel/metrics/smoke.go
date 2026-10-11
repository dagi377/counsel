package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// DefaultCheckTimeout bounds one smoke check so a hung dependency never stalls the status page.
const DefaultCheckTimeout = 10 * time.Second

var _ Checks = (*SmokeChecks)(nil)

// SmokeChecks is the production Checks: apps register one check per core area at boot and
// the status page runs them. A check must exercise only the cell's own stores, so it keeps
// passing while optional dependencies (agent platform, model gateway) are unreachable.
type SmokeChecks struct {
	mu      sync.Mutex
	checks  map[string]CheckFunc
	timeout time.Duration
}

// NewSmokeChecks returns an empty SmokeChecks. A timeout <= 0 uses DefaultCheckTimeout.
func NewSmokeChecks(timeout time.Duration) *SmokeChecks {
	if timeout <= 0 {
		timeout = DefaultCheckTimeout
	}
	return &SmokeChecks{checks: map[string]CheckFunc{}, timeout: timeout}
}

// Add implements Checks.
func (s *SmokeChecks) Add(name string, fn CheckFunc) error {
	if name == "" || fn == nil {
		return fmt.Errorf("add check %q: name and func are required", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.checks[name]; ok {
		return fmt.Errorf("add check %q: %w", name, ErrDuplicateCheck)
	}
	s.checks[name] = fn
	return nil
}

// Run implements Checks. Checks run in name order, each under its own timeout. A failure is
// logged by name only: the error text may carry customer data, so it never leaves this func.
// rule: ARC-U-028
func (s *SmokeChecks) Run(ctx context.Context) Status {
	s.mu.Lock()
	names := make([]string, 0, len(s.checks))
	for n := range s.checks {
		names = append(names, n)
	}
	fns := make(map[string]CheckFunc, len(s.checks))
	for n, fn := range s.checks {
		fns[n] = fn
	}
	s.mu.Unlock()
	sort.Strings(names)

	st := Status{Checks: make([]CheckResult, 0, len(names))}
	for _, n := range names {
		start := time.Now()
		err := s.runOne(ctx, fns[n])
		res := CheckResult{Name: n, OK: err == nil, Took: time.Since(start)}
		if !res.OK {
			st.Degraded = true
			slog.WarnContext(ctx, "smoke check failed", "check", n)
		}
		st.Checks = append(st.Checks, res)
	}
	return st
}

func (s *SmokeChecks) runOne(ctx context.Context, fn CheckFunc) error {
	var err error
	cctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	// Buffered so a check that ignores ctx and finishes late does not block forever.
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- ErrCheckFailed
			}
		}()
		done <- fn(cctx)
	}()
	select {
	case err = <-done:
	case <-cctx.Done():
		err = cctx.Err()
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCheckFailed, err)
	}
	return nil
}
