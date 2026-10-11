package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/jackc/pgx/v5"
)

const (
	sfMatter   = "b1111111-1111-4111-8111-111111111111"
	sfWalled   = "b2222222-2222-4222-8222-222222222222"
	sfDraft    = "b3333333-3333-4333-8333-333333333333"
	sfWallDft  = "b4444444-4444-4444-8444-444444444444"
	sfOther    = "b5555555-5555-4555-8555-555555555555"
	sfRun      = "run-submit"
	sfNoPlan   = "run-submit-noplan"
	sfKind     = "docs.outgoing"
	sfOffKind  = "billing.refund"
	sfApprover = "user-partner"
)

var errNoSubject = errors.New("test: no such subject")

// fakeSubjects puts sfDraft and sfOther on sfMatter and sfWallDft on sfWalled.
type fakeSubjects struct{}

func (fakeSubjects) SubjectMatter(_ context.Context, _, id string) (string, error) {
	switch id {
	case sfDraft, sfOther:
		return sfMatter, nil
	case sfWallDft:
		return sfWalled, nil
	}
	return "", errNoSubject
}

type submitWorld struct {
	reg   *agenttools.Registry
	gates *gatefake.Fake
}

// newSubmitWorld registers the real submit_for_approval on a Registry over module fakes.
// sfRun and sfNoPlan exist; only sfRun reported a plan. clerk holds actorCaps and is walled
// from sfWalled.
func newSubmitWorld(t *testing.T, actorCaps map[string]bool) submitWorld {
	t.Helper()
	ctx := context.Background()
	walls := rolesfake.New()
	walls.Wall(clerk, sfWalled)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	g := gatefake.New(func() time.Time { return now }, gatefake.Caps{clerk: {"docs.approve"}, sfApprover: {"docs.approve"}})
	if err := g.Register(gate.GateKind{
		Slug: sfKind, Approver: "docs.approve", Risk: gate.RiskMedium, MinApprovers: 1, TTL: 48 * time.Hour,
		Effect: func(context.Context, pgx.Tx, gate.Approved) error { return nil },
	}); err != nil {
		t.Fatalf("register kind: %v", err)
	}
	open := agenttools.GateOpener(func(ctx context.Context, kind string, s registry.Subject, payload json.RawMessage, preparer registry.Principal) (gate.GateID, error) {
		return g.Open(ctx, nil, kind, s, payload, preparer)
	})
	tool, err := agent.SubmitForApprovalHandler().Build(agent.NewDeps(can, walls, fakeSubjects{}, open))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(ctx, sfRun, []string{"draft", "submit"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	reg := agenttools.NewRegistry(plans)
	if err := reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return submitWorld{reg: reg, gates: g}
}

func sfInput(subject, kind string) string {
	return `{"subject_type":"document_draft","subject_id":"` + subject + `","gate_kind":"` + kind + `","note":"please review"}`
}

// rule: ARC-I-035
// rule: ARC-I-036
// rule: ARC-I-039
func TestARC_I_035_SubmitForApproval(t *testing.T) {
	ask := map[string]bool{agenttools.CapAgentAsk: true}
	grant := []string{agenttools.CapAgentAsk}
	agentP := func(run string, g []string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: run, ActingFor: clerk, Grant: g}
	}
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}
	scoped := func(run string) context.Context {
		s := agenttools.NewScope(run, []string{sfKind})
		s.AddDraft(sfDraft)
		s.AddDraft(sfWallDft)
		return agenttools.WithScope(context.Background(), s)
	}
	cases := []struct {
		name      string
		ctx       context.Context
		p         registry.Principal
		actorCaps map[string]bool
		in        string
		wantErr   error
	}{
		{"agent submits its draft", scoped(sfRun), agentP(sfRun, grant), ask, sfInput(sfDraft, sfKind), nil},
		{"manual path", context.Background(), user, ask, sfInput(sfOther, sfKind), nil},
		{"gate kind outside the run scope", scoped(sfRun), agentP(sfRun, grant), ask, sfInput(sfDraft, sfOffKind), agenttools.ErrGateKindNotAllowed},
		{"subject not drafted in this run", scoped(sfRun), agentP(sfRun, grant), ask, sfInput(sfOther, sfKind), agenttools.ErrNotFound},
		{"no run scope fails closed", context.Background(), agentP(sfRun, grant), ask, sfInput(sfDraft, sfKind), agenttools.ErrGateKindNotAllowed},
		{"walled matter", scoped(sfRun), agentP(sfRun, grant), ask, sfInput(sfWallDft, sfKind), agent.ErrNotVisible},
		{"walled matter on the manual path", context.Background(), user, ask, sfInput(sfWallDft, sfKind), agent.ErrNotVisible},
		{"run without the capability", scoped(sfRun), agentP(sfRun, nil), ask, sfInput(sfDraft, sfKind), agenttools.ErrMissingCapability},
		{"acting user without the capability", scoped(sfRun), agentP(sfRun, grant), map[string]bool{}, sfInput(sfDraft, sfKind), agent.ErrForbidden},
		{"no report_plan in the run", scoped(sfNoPlan), agentP(sfNoPlan, grant), ask, sfInput(sfDraft, sfKind), agenttools.ErrPlanRequired},
		{"input without gate_kind fails the a2 schema", scoped(sfRun), agentP(sfRun, grant), ask, `{"subject_type":"document_draft","subject_id":"` + sfDraft + `"}`, agenttools.ErrInvalidInput},
		{"decision field is not in the schema", scoped(sfRun), agentP(sfRun, grant), ask, `{"subject_type":"document_draft","subject_id":"` + sfDraft + `","gate_kind":"` + sfKind + `","approve":true}`, agenttools.ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newSubmitWorld(t, tc.actorCaps)
			res, err := w.reg.Call(tc.ctx, tc.p, agent.ToolSubmitForApproval, json.RawMessage(tc.in))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			// rule: ARC-I-035
			if err := agenttools.CheckWriteResult(res); err != nil {
				t.Fatalf("result: %v", err)
			}
			var out agenttools.WriteOutput
			if err := json.Unmarshal(res.Output, &out); err != nil || out.Outcome != agenttools.OutcomeGate {
				t.Fatalf("output = %s, want a gate outcome", res.Output)
			}
			g, err := w.gates.Get(context.Background(), gate.GateID(res.GateID))
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			if g.Kind != sfKind || g.Status != gate.StatusPending {
				t.Fatalf("gate = %s %s, want pending %s", g.Kind, g.Status, sfKind)
			}
			// rule: ARC-I-039
			// The agent never decides; the acting user prepared the gate and cannot either.
			d := gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)}
			if err := w.gates.Decide(context.Background(), nil, g.ID, g.Version, d, agentP(sfRun, grant)); !errors.Is(err, gate.ErrAgentApprover) {
				t.Fatalf("agent decide err = %v, want ErrAgentApprover", err)
			}
			if err := w.gates.Decide(context.Background(), nil, g.ID, g.Version, d, user); !errors.Is(err, gate.ErrSelfApproval) {
				t.Fatalf("preparer decide err = %v, want ErrSelfApproval", err)
			}
			if err := w.gates.Decide(context.Background(), nil, g.ID, g.Version, d, registry.Principal{Kind: registry.PrincipalUser, UserID: sfApprover}); err != nil {
				t.Fatalf("approver decide: %v", err)
			}
		})
	}
}

func TestSubmitForApprovalMissingDeps(t *testing.T) {
	if _, err := agent.SubmitForApprovalHandler().Build(agent.NewDeps()); !errors.Is(err, agent.ErrMissingDep) {
		t.Fatalf("build with no deps err = %v, want ErrMissingDep", err)
	}
}
