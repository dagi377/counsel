package boot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	agentrun "github.com/dagi377/counsel/kernel/agenttools/run"
)

// RunRecords is the part of the run record store a runner needs before a run starts.
type RunRecords interface {
	Get(ctx context.Context, id string) (agentrun.Record, error)
	Create(ctx context.Context, r agentrun.Record) error
}

// RecordFirstRunner records a run in agent.run_record before it runs, when nothing has
// recorded it yet. A screen action, a scheduled job, an event trigger and a retry all run
// through it, so a tool that reads the run (draft_bill's preparer is the run's acting_for)
// always finds it. Without a store, or when the record cannot be written, nothing runs.
type RecordFirstRunner struct {
	agentrun.Runner
	Records RunRecords
	Now     func() time.Time
}

// RecordFirst wraps r so every run it starts is recorded first.
func RecordFirst(records RunRecords, r agentrun.Runner) agentrun.Runner {
	if r == nil {
		return nil
	}
	return RecordFirstRunner{Runner: r, Records: records}
}

// Run implements the run Runner.
func (r RecordFirstRunner) Run(ctx context.Context, spec agentrun.Spec, p agentrun.Principal, prompt string, triggers ...agentrun.Trigger) (agentrun.Outcome, error) {
	if err := r.record(ctx, spec, p); err != nil {
		return agentrun.Outcome{}, fmt.Errorf("run %s: %w", spec.FeatureID, err)
	}
	return r.Runner.Run(ctx, spec, p, prompt, triggers...)
}

// record stores the run as queued unless it is already recorded (RunStarter records its own
// runs, and a retry resumes a recorded run).
func (r RecordFirstRunner) record(ctx context.Context, spec agentrun.Spec, p agentrun.Principal) error {
	if r.Records == nil {
		return fmt.Errorf("run records: %w", agent.ErrMissingDep)
	}
	if p.RunID == "" || p.ActingFor == "" {
		return fmt.Errorf("record run: %w", agentrun.ErrNotAgent)
	}
	_, err := r.Records.Get(ctx, p.RunID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, agentrun.ErrRecordNotFound) {
		return fmt.Errorf("record run %s: %w", p.RunID, err)
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	// rule: AI-R-005
	// The record holds ids, state and grant only; never the prompt or the trigger text.
	rec := agentrun.Record{ID: p.RunID, FeatureID: spec.FeatureID, SkillID: strings.ToLower(spec.FeatureID),
		ActingFor: p.ActingFor, Grant: slices.Clone(p.Grant), State: agentrun.StateQueued, CreatedAt: now()}
	if err := r.Records.Create(ctx, rec); err != nil {
		return fmt.Errorf("record run %s: %w", p.RunID, err)
	}
	return nil
}
