// Package metricsfake is an in-memory metrics.Recorder and metrics.Checks for tests.
package metricsfake

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/metrics"
)

var (
	_ metrics.Recorder = (*Fake)(nil)
	_ metrics.Checks   = (*Fake)(nil)
)

// Fake counts metric samples by name and keeps smoke checks in memory.
type Fake struct {
	mu      sync.Mutex
	counts  map[string]int
	samples map[string][]time.Duration
	checks  map[string]metrics.CheckFunc
}

// New returns an empty Fake.
func New() *Fake {
	return &Fake{counts: map[string]int{}, samples: map[string][]time.Duration{}, checks: map[string]metrics.CheckFunc{}}
}

func checkLabels(labels map[string]string) error {
	for k := range labels {
		if !slices.Contains(metrics.AllowedLabels, k) {
			return fmt.Errorf("label %q: %w", k, metrics.ErrLabel)
		}
	}
	return nil
}

// Inc implements metrics.Recorder.
func (f *Fake) Inc(_ context.Context, name string, labels map[string]string) error {
	if err := checkLabels(labels); err != nil {
		return fmt.Errorf("inc %s: %w", name, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[name]++
	return nil
}

// Observe implements metrics.Recorder.
func (f *Fake) Observe(_ context.Context, name string, d time.Duration, labels map[string]string) error {
	if err := checkLabels(labels); err != nil {
		return fmt.Errorf("observe %s: %w", name, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.samples[name] = append(f.samples[name], d)
	return nil
}

// Count returns how many times Inc recorded name.
func (f *Fake) Count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[name]
}

// Samples returns the durations Observe recorded for name.
func (f *Fake) Samples(name string) []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.samples[name])
}

// Add implements metrics.Checks.
func (f *Fake) Add(name string, fn metrics.CheckFunc) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.checks[name]; ok {
		return fmt.Errorf("add check %q: %w", name, metrics.ErrDuplicateCheck)
	}
	f.checks[name] = fn
	return nil
}

// Run implements metrics.Checks. Took is always zero so results are deterministic.
func (f *Fake) Run(ctx context.Context) metrics.Status {
	f.mu.Lock()
	names := make([]string, 0, len(f.checks))
	for n := range f.checks {
		names = append(names, n)
	}
	fns := f.checks
	f.mu.Unlock()
	sort.Strings(names)
	var st metrics.Status
	for _, n := range names {
		ok := fns[n](ctx) == nil
		st.Checks = append(st.Checks, metrics.CheckResult{Name: n, OK: ok})
		if !ok {
			st.Degraded = true
		}
	}
	return st
}
