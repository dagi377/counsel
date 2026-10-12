package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/registry"
)

// RunRecords is the part of run.PGRecords the run service uses.
type RunRecords interface {
	Create(ctx context.Context, r run.Record) error
	Start(ctx context.Context, id string, at time.Time) error
	Finish(ctx context.Context, id string, at time.Time, f run.Finish) error
	Get(ctx context.Context, id string) (run.Record, error)
}

// RunStarter is the RunService over the kernel run loop: a skill is a feature definition (its id in
// lower case), the run is recorded in Records and then driven in the background as the run
// principal. Only ids, state, timestamps and model usage are recorded; the model's output is
// dropped (AI-R-005).
type RunStarter struct {
	Features *feature.Set
	Model    ai.ModelClient
	Tools    agenttools.Tools
	Records  RunRecords
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
	// Go runs a started run; nil means a new goroutine.
	Go func(func())
	// Wrap, when set, wraps each run's loop, as boot wraps every runner it drives (the run's
	// trigger values recorded as read, its provenance dropped at the end, its feature in ctx).
	Wrap func(run.Runner) run.Runner
}

var _ RunService = (*RunStarter)(nil)

// SkillID is the skill id of a feature: its id in lower case ("F-DL-01" is "f-dl-01").
func SkillID(featureID string) string { return strings.ToLower(featureID) }

func (s *RunStarter) def(skillID string) (feature.Def, error) {
	if s.Features != nil {
		for _, d := range s.Features.All() {
			if SkillID(d.ID) == skillID {
				return d, nil
			}
		}
	}
	return feature.Def{}, fmt.Errorf("skill %q: %w", skillID, ErrUnknownSkill)
}

func (s *RunStarter) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// SkillGrant implements RunService.
func (s *RunStarter) SkillGrant(_ context.Context, skillID string) ([]string, error) {
	d, err := s.def(skillID)
	if err != nil {
		return nil, err
	}
	return slices.Clone(d.Grant), nil
}

// Start implements RunService: it records the run as queued and starts it in the background.
func (s *RunStarter) Start(ctx context.Context, x StartRun) (AgentRun, error) {
	// rule: ARC-I-036
	// A run acts only as the agent principal the route built; anything else is refused.
	p := x.Principal
	if p.Kind != registry.PrincipalAgent || p.RunID != x.ID || p.ActingFor == "" {
		return AgentRun{}, fmt.Errorf("start run %s: %w", x.ID, run.ErrNotAgent)
	}
	d, err := s.def(x.SkillID)
	if err != nil {
		return AgentRun{}, err
	}
	spec, err := d.Spec()
	if err != nil {
		return AgentRun{}, fmt.Errorf("start run %s: %w", x.ID, err)
	}
	// The run holds the narrowed grant, never the feature's full grant.
	spec.Grant = slices.Clone(p.Grant)
	rec := run.Record{ID: x.ID, FeatureID: d.ID, SkillID: x.SkillID, ActingFor: p.ActingFor,
		Grant: slices.Clone(p.Grant), SubjectID: x.MatterID, State: run.StateQueued, CreatedAt: s.now()}
	// rule: AI-R-005
	// The record holds ids, state and grant only; the inputs go to the model, not the store.
	if err := s.Records.Create(ctx, rec); err != nil {
		return AgentRun{}, err
	}
	inputs := string(x.Inputs)
	bg := context.WithoutCancel(ctx)
	goFn := s.Go
	if goFn == nil {
		goFn = func(f func()) { go f() }
	}
	goFn(func() { s.drive(bg, rec, spec, p, inputs) })
	return runOf(rec), nil
}

// usageSum adds up the model usage of one run.
type usageSum struct {
	mu sync.Mutex
	u  ai.Usage
}

func (u *usageSum) Record(_ context.Context, r ai.UsageRecord) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.u = u.u.Add(r.Usage)
	return nil
}

// drive runs rec to its end and records how it ended. The outcome's text never leaves here.
func (s *RunStarter) drive(ctx context.Context, rec run.Record, spec run.Spec, p registry.Principal, inputs string) {
	if err := s.Records.Start(ctx, rec.ID, s.now()); err != nil {
		slog.ErrorContext(ctx, "agent run start not recorded", "run_id", rec.ID, "feature", rec.FeatureID, "err", err)
		return
	}
	sum := &usageSum{}
	var loop run.Runner = run.New(ai.WithUsage(s.Model, sum), s.Tools)
	if s.Wrap != nil {
		loop = s.Wrap(loop)
	}
	var triggers []run.Trigger
	if inputs != "" {
		// rule: ARC-I-038
		// The caller's inputs are quoted data, never instructions.
		triggers = append(triggers, run.Trigger{Text: inputs})
	}
	out, err := loop.Run(ctx, spec, p, "skill: "+rec.SkillID, triggers...)
	f := run.Finish{State: run.StateSucceeded}
	switch {
	case err != nil:
		f = run.Finish{State: run.StateFailed, ErrorCode: failCode(err)}
	case out.RetryQueued:
		f = run.Finish{State: run.StateFailed, ErrorCode: "model_unavailable"}
	}
	sum.mu.Lock()
	f.InTokens, f.OutTokens, f.CostMicroC = int64(sum.u.InputTokens), int64(sum.u.OutputTokens), sum.u.CostMicroCents
	sum.mu.Unlock()
	if err := s.Records.Finish(ctx, rec.ID, s.now(), f); err != nil {
		slog.ErrorContext(ctx, "agent run end not recorded", "run_id", rec.ID, "feature", rec.FeatureID, "err", err)
	}
}

// failCode is the machine code a failed run records. Error text is never stored.
func failCode(err error) string {
	switch {
	case errors.Is(err, run.ErrNoEndTurn):
		return "no_end_turn"
	case errors.Is(err, run.ErrTimeout):
		return "model_timeout"
	case errors.Is(err, run.ErrBadOutput):
		return "bad_output"
	case errors.Is(err, run.ErrBadSpec):
		return "bad_spec"
	case errors.Is(err, run.ErrNotAgent):
		return "not_agent"
	}
	return "internal"
}

// Run implements RunService.
func (s *RunStarter) Run(ctx context.Context, id string) (AgentRun, error) {
	rec, err := s.Records.Get(ctx, id)
	if errors.Is(err, run.ErrRecordNotFound) {
		return AgentRun{}, fmt.Errorf("run %s: %w", id, ErrRunNotFound)
	}
	if err != nil {
		return AgentRun{}, err
	}
	return runOf(rec), nil
}

func runOf(r run.Record) AgentRun {
	return AgentRun{ID: r.ID, SkillID: r.SkillID, ActingFor: r.ActingFor, MatterID: r.SubjectID, State: r.State,
		Grant: slices.Clone(r.Grant), CreatedAt: r.CreatedAt}
}
