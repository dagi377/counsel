package boot

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/agenttools"
	agentrun "github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

// Feature run errors. A job that fails with one of them is cancelled, never retried.
var (
	// ErrUnknownFeature: the job names a feature this process does not define.
	ErrUnknownFeature = errors.New("legal boot: agent feature not defined")
	// ErrNoActingUser: the job names no user to act for, and a scheduled job's feature has no
	// designated lawyer.
	ErrNoActingUser = errors.New("legal boot: agent run has no acting user")
)

// RunPrincipals builds the agent principal a run acts as; identity.Checker is one.
type RunPrincipals interface {
	AgentPrincipal(ctx context.Context, run identity.RunID, actingFor identity.UserID, grant []string) (registry.Principal, error)
}

// FeatureRunWorker runs agent_feature_run jobs: a queued screen action, a kernel event or a
// schedule slot. Each job runs its feature once, as an agent acting for the job's user (a
// scheduled job's designated lawyer), on the production model and tool registry, and records
// the run in Records. The run id is derived from the River job id, so a retried job resumes
// the same run record and its drafts reuse their rows through the run's idempotency keys.
type FeatureRunWorker struct {
	river.WorkerDefaults[trigger.RunArgs]
	Features   *feature.Set
	Access     RunPrincipals
	Model      ai.ModelClient
	Tools      agenttools.Tools
	Provenance *agenttools.Provenance
	Records    agent.RunRecords
	// Designated names the lawyer a scheduled job acts for when the job names nobody.
	Designated trigger.DesignatedLawyers
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
}

// JobRunID is the run id of River job jobID: a UUID derived from the job id, so every
// attempt of one job is the same run.
func JobRunID(jobID int64) string {
	sum := sha256.Sum256([]byte(trigger.JobKindRun + "\x00" + strconv.FormatInt(jobID, 10)))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x80 // version 8: a custom, name-based UUID
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Work implements river.Worker.
func (w *FeatureRunWorker) Work(ctx context.Context, job *river.Job[trigger.RunArgs]) error {
	last := job.MaxAttempts > 0 && job.Attempt >= job.MaxAttempts
	err := w.Run(ctx, JobRunID(job.ID), job.Args, last)
	if errors.Is(err, ErrUnknownFeature) || errors.Is(err, ErrNoActingUser) || errors.Is(err, agentrun.ErrNotAgent) {
		return river.JobCancel(err)
	}
	return err
}

// errRetryLater ends an attempt whose model was unavailable, so River retries the job.
var errRetryLater = errors.New("legal boot: model unavailable, run retried later")

// Run runs args as run runID. A run already ended returns nil and runs nothing (a redelivered
// job). A transient model fault returns an error so the job is retried, unless last is set:
// then the run ends failed.
func (w *FeatureRunWorker) Run(ctx context.Context, runID string, args trigger.RunArgs, last bool) error {
	if w.Features == nil || w.Access == nil || w.Model == nil || w.Tools == nil || w.Records == nil {
		return fmt.Errorf("feature run %s: %w", args.FeatureID, agent.ErrMissingDep)
	}
	d, ok := w.Features.ByID(args.FeatureID)
	if !ok {
		return fmt.Errorf("feature run %s: %w", args.FeatureID, ErrUnknownFeature)
	}
	rec, err := w.Records.Get(ctx, runID)
	switch {
	case err == nil && (rec.State == agentrun.StateSucceeded || rec.State == agentrun.StateFailed):
		// The job ran to its end before; a redelivery runs nothing.
		return nil
	case err != nil && !errors.Is(err, agentrun.ErrRecordNotFound):
		return fmt.Errorf("feature run %s: %w", args.FeatureID, err)
	}
	found := err == nil
	actingFor, err := w.actingFor(ctx, args)
	if err != nil {
		return err
	}
	spec, err := d.Spec()
	if err != nil {
		return fmt.Errorf("feature run %s: %w", d.ID, err)
	}
	// rule: ARC-I-036
	// The run acts as an agent for the acting user, with the feature's grant narrowed to what
	// that user holds; it never acts in its own name.
	p, err := w.Access.AgentPrincipal(ctx, identity.RunID(runID), identity.UserID(actingFor), spec.Grant)
	if err != nil {
		return fmt.Errorf("feature run %s: principal: %w", d.ID, err)
	}
	if p.Kind != registry.PrincipalAgent || p.RunID != runID || p.ActingFor == "" {
		return fmt.Errorf("feature run %s: %w", d.ID, agentrun.ErrNotAgent)
	}
	spec.Grant = append([]string(nil), p.Grant...)
	if !found {
		// rule: AI-R-005
		// The record holds ids, state and grant only; no prompt or model text.
		rec = agentrun.Record{ID: runID, FeatureID: d.ID, SkillID: agent.SkillID(d.ID), ActingFor: p.ActingFor,
			Grant: spec.Grant, SubjectID: subjectID(args.Subjects), State: agentrun.StateQueued, CreatedAt: w.now()}
		if err := w.Records.Create(ctx, rec); err != nil {
			return fmt.Errorf("feature run %s: %w", d.ID, err)
		}
	}
	if rec.State == agentrun.StateQueued {
		if err := w.Records.Start(ctx, runID, w.now()); err != nil {
			return fmt.Errorf("feature run %s: %w", d.ID, err)
		}
	}
	sum := &runUsage{}
	runner := ForgetOnEnd(w.Provenance, agentrun.New(ai.WithUsage(w.Model, sum), w.Tools))
	out, runErr := runner.Run(ctx, spec, p, runPrompt(d.ID, args))
	for _, c := range out.Calls {
		if c.Refused != "" {
			// The machine code only; the refusal's text may echo tool input.
			slog.WarnContext(ctx, "agent run tool call refused", "run_id", runID, "feature", d.ID, "tool", c.Tool, "code", c.Refused)
		}
	}
	f := agentrun.Finish{State: agentrun.StateSucceeded}
	switch {
	case runErr != nil:
		f = agentrun.Finish{State: agentrun.StateFailed, ErrorCode: runFailCode(runErr)}
		slog.WarnContext(ctx, "agent feature run failed", "run_id", runID, "feature", d.ID, "code", f.ErrorCode)
	case out.RetryQueued && !last:
		// The record stays running; the retried job resumes it.
		return fmt.Errorf("feature run %s: %w", d.ID, errRetryLater)
	case out.RetryQueued:
		f = agentrun.Finish{State: agentrun.StateFailed, ErrorCode: "model_unavailable"}
	}
	f.InTokens, f.OutTokens, f.CostMicroC = sum.totals()
	if err := w.Records.Finish(ctx, runID, w.now(), f); err != nil {
		return fmt.Errorf("feature run %s: %w", d.ID, err)
	}
	slog.InfoContext(ctx, "agent feature run ended", "run_id", runID, "feature", d.ID, "state", f.State, "gates", len(out.GateIDs))
	return nil
}

// actingFor is the user the job acts for: the job's user, else for a scheduled job the
// feature's designated lawyer. No user fails closed.
func (w *FeatureRunWorker) actingFor(ctx context.Context, args trigger.RunArgs) (string, error) {
	if args.ActingFor != "" {
		return args.ActingFor, nil
	}
	if args.Slot != "" && w.Designated != nil {
		// rule: ARC-I-040
		// A scheduled run with no user acts for the feature's designated lawyer.
		u, err := w.Designated.DesignatedLawyer(ctx, args.FeatureID)
		if err != nil && !errors.Is(err, trigger.ErrNoDesignatedLawyer) {
			return "", fmt.Errorf("feature run %s: designated lawyer: %w", args.FeatureID, err)
		}
		if u != "" {
			return u, nil
		}
	}
	return "", fmt.Errorf("feature run %s: %w", args.FeatureID, ErrNoActingUser)
}

func (w *FeatureRunWorker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// subjectID is the run record's subject: the first matter the job is about, if any.
func subjectID(subjects []trigger.SubjectRef) string {
	for _, s := range subjects {
		if s.Kind == "matter" && isUUID(s.ID) {
			return s.ID
		}
	}
	return ""
}

// runPrompt is a job run's prompt: the feature, its records and its slot, ids only.
func runPrompt(featureID string, args trigger.RunArgs) string {
	ids := make([]string, 0, len(args.Subjects))
	for _, s := range args.Subjects {
		ids = append(ids, s.Kind+":"+s.ID)
	}
	out := "feature: " + featureID
	if len(ids) > 0 {
		out += "\nrecords: " + strings.Join(ids, ", ")
	}
	if args.Slot != "" {
		out += "\nslot: " + args.Slot
	}
	return out
}

// runFailCode is the machine code a failed run records. Error text is never stored.
func runFailCode(err error) string {
	switch {
	case errors.Is(err, agentrun.ErrNoEndTurn):
		return "no_end_turn"
	case errors.Is(err, agentrun.ErrTimeout):
		return "model_timeout"
	case errors.Is(err, agentrun.ErrBadOutput):
		return "bad_output"
	case errors.Is(err, agentrun.ErrBadSpec):
		return "bad_spec"
	case errors.Is(err, agentrun.ErrNotAgent):
		return "not_agent"
	}
	return "internal"
}

// runUsage adds up the model usage of one run.
type runUsage struct {
	mu sync.Mutex
	u  ai.Usage
}

// Record implements ai.UsageRecorder.
func (u *runUsage) Record(_ context.Context, r ai.UsageRecord) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.u = u.u.Add(r.Usage)
	return nil
}

func (u *runUsage) totals() (in, out, cost int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return int64(u.u.InputTokens), int64(u.u.OutputTokens), u.u.CostMicroCents
}
