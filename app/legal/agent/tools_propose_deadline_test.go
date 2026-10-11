package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	dlMatter  = "d1111111-1111-4111-8111-111111111111"
	dlWalled  = "d2222222-2222-4222-8222-222222222222"
	dlRun     = "run-deadline"
	dlNoPlan  = "run-deadline-noplan"
	dlCap     = "deadlines.edit"
	dlDue     = "2026-11-02"
	dlTrigger = "2026-10-03"
	dlLawyer  = "user-lawyer"
)

var dlNow = time.Date(2026, 10, 11, 13, 0, 0, 0, time.UTC)

// countingSaver is the deadline fake, counting what was stored.
type countingSaver struct {
	*deadlinefake.Fake
	saved []deadline.DeadlineID
}

func (c *countingSaver) Save(ctx context.Context, tx pgx.Tx, p deadline.Proposed) (deadline.DeadlineID, error) {
	id, err := c.Fake.Save(ctx, tx, p)
	if err == nil {
		c.saved = append(c.saved, id)
	}
	return id, err
}

type deadlineWorld struct {
	reg       *agenttools.Registry
	deadlines *countingSaver
	gates     *gatefake.Fake
}

// newDeadlineWorld registers the real propose_deadline over module fakes. dlRun has a plan
// and has read dlDue and dlTrigger (as compute_deadline would return them); dlNoPlan has
// neither. clerk holds actorCaps and is walled from dlWalled.
func newDeadlineWorld(t *testing.T, actorCaps map[string]bool) deadlineWorld {
	t.Helper()
	ctx := context.Background()
	walls := rolesfake.New()
	walls.Wall(clerk, dlWalled)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	w := deadlineWorld{deadlines: &countingSaver{Fake: deadlinefake.New()}}
	w.gates = gatefake.New(func() time.Time { return dlNow }, gatefake.Caps{dlLawyer: {"deadlines.verify"}, clerk: {"deadlines.verify"}})
	if err := w.gates.Register(gate.GateKind{
		Slug: agent.GateCalConfirm, Approver: "deadlines.verify", Risk: gate.RiskHigh, MinApprovers: 1, TTL: 24 * time.Hour,
		PayloadSchema: []byte(`{"type":"object"}`),
		Effect:        func(context.Context, pgx.Tx, gate.Approved) error { return nil },
	}); err != nil {
		t.Fatalf("register gate kind: %v", err)
	}
	open := agenttools.GateOpener(func(ctx context.Context, kind string, s registry.Subject, payload json.RawMessage, p agenttools.Principal) (gate.GateID, error) {
		return w.gates.Open(ctx, nil, kind, s, payload, p)
	})
	pv := agenttools.NewProvenance()
	pv.Observe(dlRun, json.RawMessage(`{"due_on":"`+dlDue+`","trigger_date":"`+dlTrigger+`"}`))
	tool, err := agent.ProposeDeadlineHandler().Build(agent.NewDeps(can, walls, w.deadlines, open, pv))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(ctx, dlRun, []string{"propose the filing deadline"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	w.reg = agenttools.NewRegistry(plans)
	if err := w.reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return w
}

func deadlineInput(matterID, due string, extra map[string]any) string {
	in := map[string]any{
		"matter_id": matterID, "title": "Serve affidavit of documents", "due_on": due,
		"source": map[string]any{
			"doc_id": "e1111111-1111-4111-8111-111111111111", "version_id": "e2222222-2222-4222-8222-222222222222",
			"page": 2, "line_start": 4, "line_end": 6,
			"quote_hash": "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
	}
	for k, v := range extra {
		in[k] = v
	}
	b, _ := json.Marshal(in)
	return string(b)
}

// rule: ARC-I-035
// rule: ARC-I-036
// rule: ARC-I-039
func TestARC_I_035_ProposeDeadline(t *testing.T) {
	edit := map[string]bool{dlCap: true}
	grant := []string{dlCap}
	agentP := func(run string, g []string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: run, ActingFor: clerk, Grant: g}
	}
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}
	confirmOnly := []string{agent.GateCalConfirm}
	ok := deadlineInput(dlMatter, dlDue, map[string]any{"rule_id": "rcp-30.03", "trigger_date": dlTrigger})
	cases := []struct {
		name      string
		p         registry.Principal
		scope     []string // gate kinds of the run scope; nil means no scope in ctx
		actorCaps map[string]bool
		in        string
		wantErr   error
	}{
		{"agent proposes a computed deadline", agentP(dlRun, grant), confirmOnly, edit, ok, nil},
		{"agent proposes a cited document date", agentP(dlRun, grant), confirmOnly, edit, deadlineInput(dlMatter, dlDue, nil), nil},
		{"manual path", user, nil, edit, deadlineInput(dlMatter, "2027-01-15", nil), nil},
		{"due date the run never read", agentP(dlRun, grant), confirmOnly, edit, deadlineInput(dlMatter, "2026-11-03", nil), agenttools.ErrUnsourcedValue},
		{"trigger date the run never read", agentP(dlRun, grant), confirmOnly, edit,
			deadlineInput(dlMatter, dlDue, map[string]any{"trigger_date": "2026-10-04"}), agenttools.ErrUnsourcedValue},
		{"walled matter", agentP(dlRun, grant), confirmOnly, edit, deadlineInput(dlWalled, dlDue, nil), agent.ErrNotVisible},
		{"walled matter on the manual path", user, nil, edit, deadlineInput(dlWalled, dlDue, nil), agent.ErrNotVisible},
		{"run without the capability", agentP(dlRun, nil), confirmOnly, edit, ok, agenttools.ErrMissingCapability},
		{"acting user without the capability", agentP(dlRun, grant), confirmOnly, map[string]bool{}, ok, agent.ErrForbidden},
		{"no report_plan in the run", agentP(dlNoPlan, grant), confirmOnly, edit, ok, agenttools.ErrPlanRequired},
		{"feature does not declare cal.confirm", agentP(dlRun, grant), []string{"docs.send"}, edit, ok, agenttools.ErrGateKindNotAllowed},
		{"no run scope", agentP(dlRun, grant), nil, edit, ok, agenttools.ErrGateKindNotAllowed},
		{"source missing", agentP(dlRun, grant), confirmOnly, edit, `{"matter_id":"` + dlMatter + `","title":"x","due_on":"` + dlDue + `"}`, agenttools.ErrInvalidInput},
		{"state in the input", agentP(dlRun, grant), confirmOnly, edit, deadlineInput(dlMatter, dlDue, map[string]any{"state": "confirmed"}), agenttools.ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.scope != nil {
				ctx = agenttools.WithScope(ctx, agenttools.NewScope(dlRun, tc.scope))
			}
			w := newDeadlineWorld(t, tc.actorCaps)
			res, err := w.reg.Call(ctx, tc.p, agent.ToolProposeDeadline, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				if len(w.deadlines.saved) != 0 {
					t.Errorf("refused call stored %d deadlines", len(w.deadlines.saved))
				}
				return
			}
			// rule: ARC-I-035
			if err := agenttools.CheckWriteResult(res); err != nil {
				t.Fatalf("write result: %v", err)
			}
			if len(w.deadlines.saved) != 1 {
				t.Fatalf("stored %d deadlines, want 1", len(w.deadlines.saved))
			}
			dl, err := w.deadlines.Get(ctx, w.deadlines.saved[0])
			if err != nil {
				t.Fatalf("deadline: %v", err)
			}
			if dl.State != deadline.StateProposed || dl.ConfirmedBy != "" || dl.Proposed.Subject != (registry.Subject{Kind: agent.WorkSubjectMatter, ID: dlMatter}) {
				t.Errorf("deadline = %+v, want proposed on the matter", dl)
			}
			g, err := w.gates.Get(ctx, gate.GateID(res.GateID))
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			if g.Kind != agent.GateCalConfirm || g.Status != gate.StatusPending || g.Subject != (registry.Subject{Kind: agent.SubjectDeadline, ID: string(dl.ID)}) {
				t.Errorf("gate = %+v, want a pending cal.confirm on the deadline", g)
			}
		})
	}
}

// rule: ARC-I-039
// rule: ARC-I-035
// The opened gate is decided by a person other than the acting user; the agent never decides.
func TestARC_I_039_ProposeDeadlineGateNotDecidedByAgent(t *testing.T) {
	cases := []struct {
		name    string
		by      registry.Principal
		wantErr error
	}{
		{"agent approves", registry.Principal{Kind: registry.PrincipalAgent, RunID: dlRun, ActingFor: clerk, Grant: []string{"deadlines.verify"}}, agenttools.ErrAgentDecides},
		{"agent acting for another user", registry.Principal{Kind: registry.PrincipalAgent, RunID: dlRun, ActingFor: dlLawyer, Grant: []string{"deadlines.verify"}}, agenttools.ErrAgentDecides},
		{"acting user approves", registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}, gate.ErrSelfApproval},
		{"another lawyer approves", registry.Principal{Kind: registry.PrincipalUser, UserID: dlLawyer}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := agenttools.WithScope(context.Background(), agenttools.NewScope(dlRun, []string{agent.GateCalConfirm}))
			w := newDeadlineWorld(t, map[string]bool{dlCap: true})
			p := registry.Principal{Kind: registry.PrincipalAgent, RunID: dlRun, ActingFor: clerk, Grant: []string{dlCap}}
			res, err := w.reg.Call(ctx, p, agent.ToolProposeDeadline, json.RawMessage(deadlineInput(dlMatter, dlDue, nil)))
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			id := gate.GateID(res.GateID)
			g0, err := w.gates.Get(ctx, id)
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			d := gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: g0.PayloadHash, MFAVerifiedAt: dlNow.Add(-time.Minute)}
			err = agenttools.DecideGuard{Engine: w.gates}.Decide(ctx, nil, id, g0.Version, d, tc.by)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("decide err = %v, want %v", err, tc.wantErr)
			}
			g, err := w.gates.Get(ctx, id)
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			if (g.Status == gate.StatusPending) != (tc.wantErr != nil) {
				t.Errorf("status = %s after decision by %+v", g.Status, tc.by)
			}
		})
	}
}
