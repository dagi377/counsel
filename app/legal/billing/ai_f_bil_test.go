package billing

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/fixture"
)

// bilFeature is one billing AI feature whose run ends in one proposal behind a gate. The
// shared agent driver runs it: only the model is fake.
type bilFeature struct {
	feature agenttest.Feature
	// dir is the fixture dir; a run then sees only its variant. Empty: every fixture, and
	// the prompt picks the variant.
	dir        string
	prompt     func(variant string) string
	reads      map[string]string // read tool -> text it returns in a clean run
	injectRead string            // the read tool whose text carries the injection
	injected   string            // its text in the injection variant
	injection  string            // the injected instruction
	calls      []string          // tool calls of a clean run
	planFirst  bool              // report_plan is the run's first call
	gateKind   string            // the gate the proposal opens
	specGate   string            // spec gate id, e.g. "G-B7"
	approver   string            // capability that decides the gate
	// injectedWriteErr is what refuses the trust withdrawal the injection asks for.
	injectedWriteErr error
	// check checks the proposal's payload.
	check func(t *testing.T, d *agenttest.Driver, run agenttest.Run, p agenttest.Proposal)
}

func (f bilFeature) driver(t *testing.T, inject bool) *agenttest.Driver {
	t.Helper()
	d := agenttest.New(t, f.feature)
	for tool, text := range f.reads {
		d.SetRead(tool, text)
	}
	if inject {
		d.SetRead(f.injectRead, f.injected)
	}
	return d
}

func (f bilFeature) model(t *testing.T, variant string) ai.ModelClient {
	t.Helper()
	if f.dir == "" {
		return agenttest.Models(t)
	}
	return agenttest.Model(t, f.dir, variant)
}

// run checks every required fixture variant of f.
func (f bilFeature) run(t *testing.T) {
	timeoutRetries := slices.ContainsFunc(f.feature.RetryOn, func(e error) bool { return errors.Is(e, ai.ErrTimeout) })
	for _, variant := range fixture.RequiredVariants {
		t.Run(variant, func(t *testing.T) {
			ctx := context.Background()
			inject := variant == fixture.VariantInjection
			model := f.model(t, variant)
			d := f.driver(t, inject)
			runID := "run-" + strings.ToLower(f.feature.ID) + "-" + variant
			run := d.Fire(ctx, model, runID, f.prompt(variant))

			// rule: AI-R-003
			// A failed or rate-limited call leaves no partial write.
			switch {
			case variant == fixture.VariantTimeout && !timeoutRetries:
				if !errors.Is(run.Err, ai.ErrTimeout) || run.RetryQueued {
					t.Fatalf("err = %v queued %v, want %v", run.Err, run.RetryQueued, ai.ErrTimeout)
				}
			case variant == fixture.VariantTimeout, variant == fixture.VariantError429:
				if run.Err != nil || !run.RetryQueued {
					t.Fatalf("err = %v queued %v, want a queued retry", run.Err, run.RetryQueued)
				}
			}
			if variant == fixture.VariantTimeout || variant == fixture.VariantError429 {
				if len(run.Tools) != 0 || len(run.GateIDs) != 0 {
					t.Fatalf("partial write on fault: calls %v gates %v", run.Tools, run.GateIDs)
				}
				if variant == fixture.VariantTimeout {
					return
				}
				// The queued retry reruns the same run id and now reaches the script.
				run = d.Fire(ctx, model, runID, f.prompt(variant))
			}
			if run.Err != nil || run.RetryQueued {
				t.Fatalf("run: err %v queued %v", run.Err, run.RetryQueued)
			}

			want := f.calls
			if inject {
				want = append(slices.Clone(f.calls), agent.ToolPrepareTrustWithdrawal)
			}
			// rule: ARC-I-038
			if !slices.Equal(run.Tools, want) {
				t.Fatalf("calls = %v, want %v (no extra tool call)", run.Tools, want)
			}
			if inject {
				if err := agenttest.QuotedOnly(run, f.injection); err != nil {
					t.Fatal(err)
				}
				// rule: ARC-I-036
				if err := d.RefusedInjectedWrite(ctx, run, f.injectedWriteErr); err != nil {
					t.Fatal(err)
				}
			} else if len(run.Refused) != 0 {
				t.Fatalf("refused calls: %v", run.Refused)
			}
			// rule: ARC-I-039
			if run.PlanAt < 0 || run.PlanAt > run.FirstWrite || (f.planFirst && run.PlanAt != 0) {
				t.Fatalf("report_plan at %d, first write at %d: plan must come first", run.PlanAt, run.FirstWrite)
			}
			if len(run.GateIDs) != 1 {
				t.Fatalf("gates = %v, want one", run.GateIDs)
			}
			p, ok := d.Proposal(run.GateIDs[0])
			// rule: ARC-I-035
			if !ok || p.GateKind != f.gateKind || p.Preparer != f.feature.ActingFor || p.Decision != "" {
				t.Fatalf("proposal = %+v, want pending %s prepared by %s", p, f.gateKind, f.feature.ActingFor)
			}
			if inject && strings.Contains(string(p.Input), f.injection) {
				t.Fatalf("injected text reached the proposal: %s", p.Input)
			}
			f.check(t, d, run, p)
			if run.Text == "" {
				t.Fatal("no final answer")
			}
			f.decide(t, d, p.GateID)
		})
	}
}

// decide checks who may decide the gate: never an agent principal, never the preparer.
func (f bilFeature) decide(t *testing.T, d *agenttest.Driver, gateID string) {
	t.Helper()
	bilDeciders(t, gates.Kind{Code: f.gateKind, SpecGate: f.specGate, Approver: f.approver}, f.feature.ActingFor, gateID)
	ctx := context.Background()
	if _, err := d.Decided(ctx, agent.GateDecided{GateID: gateID, Decision: agent.Approved, By: f.feature.ActingFor}); !errors.Is(err, agent.ErrSelfApproval) {
		t.Errorf("acting user decided own gate: err %v, want ErrSelfApproval", err)
	}
}

// bilDeciders checks who may decide a gate of kind an agent prepared for actor.
func bilDeciders(t *testing.T, kind gates.Kind, actor, gateID string) {
	t.Helper()
	ctx := context.Background()
	gs, err := gatesfake.New(kind)
	if err != nil {
		t.Fatal(err)
	}
	opened := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	g := gates.Gate{ID: gateID, Code: kind.Code, OpenedAt: opened,
		Preparer: gates.Principal{UserID: "agent", Agent: true, ActingFor: actor}}
	approver := []string{kind.Approver}
	tests := []struct {
		name string
		by   gates.Principal
		want int
	}{
		// rule: ARC-I-038
		{"agent principal for the actor", gates.Principal{UserID: "agent", Agent: true, ActingFor: actor, Capabilities: approver}, http.StatusForbidden},
		{"agent principal for another user", gates.Principal{UserID: "agent", Agent: true, ActingFor: "u-owner-kim", Capabilities: approver}, http.StatusForbidden},
		{"acting user", gates.Principal{UserID: actor, Capabilities: approver}, http.StatusForbidden},
		{"other approver", gates.Principal{UserID: "u-owner-kim", Capabilities: approver}, http.StatusOK},
	}
	for _, tc := range tests {
		if got := agenttest.DecisionStatus(gs.CheckDecision(ctx, g, tc.by, opened.Add(time.Hour))); got != tc.want {
			t.Errorf("%s decides: status %d, want %d", tc.name, got, tc.want)
		}
	}
}

// retryOnTimeout is the retry policy of features whose job retries a timed-out run.
var retryOnTimeout = []error{ai.ErrRateLimited, ai.ErrOverloaded, ai.ErrTimeout}

// bracketPrompt returns a prompt whose "[variant]" suffix picks the fixture.
func bracketPrompt(trigger string) func(string) string {
	return func(variant string) string { return trigger + " [" + variant + "]" }
}

// fixedPrompt returns the same trigger for every variant.
func fixedPrompt(trigger string) func(string) string {
	return func(string) string { return trigger }
}

// missingCapability is how the Registry refuses a write the run was not granted.
var missingCapability = agenttools.ErrMissingCapability
