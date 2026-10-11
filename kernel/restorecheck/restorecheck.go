// Package restorecheck runs the checks that prove a restored database is usable. A restore
// drill runs every check against the restored cell and reports pass or fail per check.
//
// Apps register their checks at boot through registry.Registry.RestoreCheck; FromRegistry
// turns such a registration into a Check, so no registry change is needed.
package restorecheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/registry"
)

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrFailed means at least one check in the report failed.
	ErrFailed = errors.New("restorecheck: check failed")
	// ErrDuplicate means two checks share a name; both are reported failed.
	ErrDuplicate = errors.New("restorecheck: duplicate check name")
	// ErrNoName means a check has an empty name.
	ErrNoName = errors.New("restorecheck: check has no name")
	// ErrNoRun means a check has no Run function.
	ErrNoRun = errors.New("restorecheck: check has no run function")
	// ErrPanic means a check panicked; the panic is reported as a failure.
	ErrPanic = errors.New("restorecheck: check panicked")
)

// Check is one restore check. Run returns nil when the restored database passes it.
type Check struct {
	Name string
	Run  func(ctx context.Context, d db.DB) error
}

// FromRegistry adapts a check registered with registry.Registry.RestoreCheck. Registered
// checks carry their own database handle, so d is not passed on.
func FromRegistry(name string, fn registry.CheckFunc) Check {
	c := Check{Name: name}
	if fn != nil {
		c.Run = func(ctx context.Context, _ db.DB) error { return fn(ctx) }
	}
	return c
}

// Result is the outcome of one check. Err is nil when the check passed.
type Result struct {
	Name     string
	Err      error
	Duration time.Duration
}

// Passed reports whether the check passed.
func (r Result) Passed() bool { return r.Err == nil }

// Report is the outcome of one run, one Result per check in the order given.
type Report struct {
	Results []Result
}

// Passed reports whether every check passed. An empty report has not proved anything,
// so it does not pass.
func (r Report) Passed() bool {
	if len(r.Results) == 0 {
		return false
	}
	for _, res := range r.Results {
		if !res.Passed() {
			return false
		}
	}
	return true
}

// Failed returns the names of the failed checks, sorted.
func (r Report) Failed() []string {
	var out []string
	for _, res := range r.Results {
		if !res.Passed() {
			out = append(out, res.Name)
		}
	}
	sort.Strings(out)
	return out
}

// Err returns nil when the report passed, else an error wrapping ErrFailed that names the
// failed checks.
func (r Report) Err() error {
	if r.Passed() {
		return nil
	}
	if len(r.Results) == 0 {
		return fmt.Errorf("no checks ran: %w", ErrFailed)
	}
	return fmt.Errorf("%d of %d failed %v: %w", len(r.Failed()), len(r.Results), r.Failed(), ErrFailed)
}

// Write prints the report, one line per check ("PASS name 12ms" or "FAIL name 3ms: reason"),
// then a summary line ("restore check: PASS 3/3" or "restore check: FAIL 2/3").
func (r Report) Write(w io.Writer) error {
	passed := 0
	for _, res := range r.Results {
		var err error
		if res.Passed() {
			passed++
			_, err = fmt.Fprintf(w, "PASS %s %s\n", res.Name, res.Duration.Round(time.Millisecond))
		} else {
			_, err = fmt.Fprintf(w, "FAIL %s %s: %v\n", res.Name, res.Duration.Round(time.Millisecond), res.Err)
		}
		if err != nil {
			return fmt.Errorf("write report: %w", err)
		}
	}
	verdict := "PASS"
	if !r.Passed() {
		verdict = "FAIL"
	}
	if _, err := fmt.Fprintf(w, "restore check: %s %d/%d\n", verdict, passed, len(r.Results)); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

// Run runs every check against d and returns one Result per check. It never stops at the
// first failure: a failing, panicking or malformed check is recorded and the next one runs.
// Checks sharing a name are all reported failed, since the report could not tell them apart.
//
// rule: ARC-U-043
func Run(ctx context.Context, d db.DB, checks []Check) Report {
	seen := map[string]int{}
	for _, c := range checks {
		seen[c.Name]++
	}
	rep := Report{Results: make([]Result, 0, len(checks))}
	for _, c := range checks {
		res := Result{Name: c.Name}
		switch {
		case c.Name == "":
			res.Err = ErrNoName
		case seen[c.Name] > 1:
			res.Err = fmt.Errorf("check %q: %w", c.Name, ErrDuplicate)
		case c.Run == nil:
			res.Err = fmt.Errorf("check %q: %w", c.Name, ErrNoRun)
		default:
			start := time.Now()
			res.Err = runOne(ctx, d, c)
			res.Duration = time.Since(start)
		}
		rep.Results = append(rep.Results, res)
	}
	return rep
}

func runOne(ctx context.Context, d db.DB, c Check) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("check %q: %w: %v", c.Name, ErrPanic, p)
		}
	}()
	if err := c.Run(ctx, d); err != nil {
		return fmt.Errorf("check %q: %w", c.Name, err)
	}
	return nil
}
