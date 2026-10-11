package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	emailMatter   = "61111111-1111-4111-8111-111111111111"
	emailWalled   = "62222222-2222-4222-8222-222222222222"
	emailClient   = "63333333-3333-4333-8333-333333333333"
	emailOpposing = "64444444-4444-4444-8444-444444444444"
	emailStranger = "65555555-5555-4555-8555-555555555555"
	emailPartner  = "user-partner"
)

// fakeEmailDrafts stores draft emails. Send exists only so the test can show that no path
// of draft_email reaches it.
type fakeEmailDrafts struct {
	mu     sync.Mutex
	drafts []agent.NewDraftEmail
	sent   int
}

func (f *fakeEmailDrafts) CreateEmailDraft(_ context.Context, d agent.NewDraftEmail) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drafts = append(f.drafts, d)
	return "email-draft-" + string(rune('0'+len(f.drafts))), nil
}

func (f *fakeEmailDrafts) Send(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent++
	return nil
}

type fakeMatterContacts map[string][]string

func (f fakeMatterContacts) MatterContactIDs(_ context.Context, matterID string) ([]string, error) {
	return f[matterID], nil
}

type emailWorld struct {
	reg    *agenttools.Registry
	drafts *fakeEmailDrafts
	gates  *gatefake.Fake
}

// newEmailWorld registers the real draft_email on a Registry over module fakes and a fake
// gate engine. Run "run-email" has a plan; "run-noplan" has none. clerk holds actorCaps and is
// walled from emailWalled; emailPartner may release documents.
func newEmailWorld(t *testing.T, actorCaps map[string]bool) emailWorld {
	t.Helper()
	ctx := context.Background()
	walls := rolesfake.New()
	walls.Wall(clerk, emailWalled)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	w := emailWorld{
		drafts: &fakeEmailDrafts{},
		gates:  gatefake.New(func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }, gatefake.Caps{emailPartner: {"documents.release"}}),
	}
	err := w.gates.Register(gate.GateKind{
		Slug: agent.GateOutgoingEmail, Approver: "documents.release", Risk: gate.RiskHigh, MinApprovers: 1, TTL: 72 * time.Hour,
		Effect: func(context.Context, pgx.Tx, gate.Approved) error { return nil },
	})
	if err != nil {
		t.Fatalf("gate kind: %v", err)
	}
	open := agenttools.GateOpener(func(ctx context.Context, kind string, s registry.Subject, payload json.RawMessage, by agenttools.Principal) (gate.GateID, error) {
		return w.gates.Open(ctx, nil, kind, s, payload, by)
	})
	contacts := fakeMatterContacts{emailMatter: {emailClient, emailOpposing}, emailWalled: {emailClient}}
	tool, err := agent.DraftEmailHandler().Build(agent.NewDeps(can, walls, w.drafts, contacts, open))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(ctx, "run-email", []string{"draft the letter"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	w.reg = agenttools.NewRegistry(plans)
	if err := w.reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return w
}

func emailInput(matterID string, to []string, extra string) string {
	b, _ := json.Marshal(to)
	return `{"matter_id":"` + matterID + `","to_contact_ids":` + string(b) + `,"subject":"Synthetic update","body_markdown":"Dear client"` + extra + `}`
}

// rule: ARC-I-035
// rule: ARC-I-036
// rule: ARC-I-039
func TestARC_I_035_DraftEmail(t *testing.T) {
	edit := map[string]bool{"documents.edit": true}
	grant := []string{"documents.edit"}
	agentP := func(run string, g []string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: run, ActingFor: clerk, Grant: g}
	}
	both := []string{emailClient, emailOpposing}
	cases := []struct {
		name      string
		p         registry.Principal
		actorCaps map[string]bool
		in        string
		wantErr   error
		wantGate  bool
	}{
		{"draft only", agentP("run-email", grant), edit, emailInput(emailMatter, both, ""), nil, false},
		{"submit false", agentP("run-email", grant), edit, emailInput(emailMatter, both, `,"submit":false`), nil, false},
		{"submit opens the outgoing-email gate", agentP("run-email", grant), edit, emailInput(emailMatter, both, `,"submit":true`), nil, true},
		{"manual path submit", registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}, edit, emailInput(emailMatter, []string{emailClient}, `,"submit":true`), nil, true},
		{"recipient not a contact of the matter", agentP("run-email", grant), edit, emailInput(emailMatter, []string{emailClient, emailStranger}, `,"submit":true`), agent.ErrNotMatterContact, false},
		{"walled matter", agentP("run-email", grant), edit, emailInput(emailWalled, []string{emailClient}, ""), agent.ErrNotVisible, false},
		{"run without the capability", agentP("run-email", nil), edit, emailInput(emailMatter, both, ""), agenttools.ErrMissingCapability, false},
		{"acting user without the capability", agentP("run-email", grant), map[string]bool{}, emailInput(emailMatter, both, ""), agent.ErrForbidden, false},
		{"no report_plan in the run", agentP("run-noplan", grant), edit, emailInput(emailMatter, both, `,"submit":true`), agenttools.ErrPlanRequired, false},
		{"no recipients", agentP("run-email", grant), edit, emailInput(emailMatter, []string{}, ""), agenttools.ErrInvalidInput, false},
		{"a send field", agentP("run-email", grant), edit, emailInput(emailMatter, both, `,"send":true`), agenttools.ErrInvalidInput, false},
		{"subject too long", agentP("run-email", grant), edit, `{"matter_id":"` + emailMatter + `","to_contact_ids":["` + emailClient + `"],"subject":"` + strings.Repeat("s", 201) + `","body_markdown":"x"}`, agenttools.ErrInvalidInput, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// rule: ARC-I-035
			// The Runner's scope for a run whose feature declares agent.GateOutgoingEmail.
			ctx := agenttools.WithScope(context.Background(), agenttools.NewScope("run-email", []string{agent.GateOutgoingEmail}))
			w := newEmailWorld(t, tc.actorCaps)
			res, err := w.reg.Call(ctx, tc.p, agent.ToolDraftEmail, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if w.drafts.sent != 0 {
				t.Fatalf("draft_email sent %d emails", w.drafts.sent)
			}
			if err != nil {
				if len(w.drafts.drafts) != 0 {
					t.Errorf("refused call stored %d drafts", len(w.drafts.drafts))
				}
				return
			}
			// rule: ARC-I-035
			if err := agenttools.CheckWriteResult(res); err != nil {
				t.Fatalf("write result: %v", err)
			}
			if len(w.drafts.drafts) != 1 || w.drafts.drafts[0].PreparedBy != clerk {
				t.Fatalf("drafts = %+v, want one prepared by the acting user", w.drafts.drafts)
			}
			var out agenttools.WriteOutput
			if err := json.Unmarshal(res.Output, &out); err != nil {
				t.Fatalf("output: %v", err)
			}
			if !tc.wantGate {
				if out.Outcome != agenttools.OutcomeDraft || res.GateID != "" {
					t.Errorf("result = %s gate %q, want a draft and no gate", res.Output, res.GateID)
				}
				return
			}
			g, err := w.gates.Get(ctx, gate.GateID(res.GateID))
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			if out.Outcome != agenttools.OutcomeGate || g.Kind != agent.GateOutgoingEmail || g.Status != gate.StatusPending ||
				g.Subject != (registry.Subject{Kind: agent.SubjectEmailDraft, ID: "email-draft-1"}) {
				t.Errorf("gate = %+v, result %s", g, res.Output)
			}
			if g.Preparer.Kind != tc.p.Kind || g.Preparer.ActingFor != tc.p.ActingFor || g.Preparer.UserID != tc.p.UserID {
				t.Errorf("preparer = %+v, want the caller %+v", g.Preparer, tc.p)
			}
			if strings.Contains(string(g.Payload), "submit") {
				t.Errorf("payload carries submit: %s", g.Payload)
			}
		})
	}
}

// rule: ARC-I-039
// The gate a submitted draft opens is decided by a person: the agent principal is refused
// and the gate stays pending; a partner other than the preparer can approve it.
func TestARC_I_039_DraftEmailGateNotDecidedByAgent(t *testing.T) {
	agentP := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-email", ActingFor: clerk, Grant: []string{"documents.edit", "documents.release"}}
	cases := []struct {
		name    string
		by      registry.Principal
		refused bool
	}{
		{"agent run approves", agentP, true},
		{"agent acting for the partner approves", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-x", ActingFor: emailPartner, Grant: []string{"documents.release"}}, true},
		{"partner approves", registry.Principal{Kind: registry.PrincipalUser, UserID: emailPartner}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// rule: ARC-I-035
			// The Runner's scope for a run whose feature declares agent.GateOutgoingEmail.
			ctx := agenttools.WithScope(context.Background(), agenttools.NewScope("run-email", []string{agent.GateOutgoingEmail}))
			w := newEmailWorld(t, map[string]bool{"documents.edit": true})
			res, err := w.reg.Call(ctx, agentP, agent.ToolDraftEmail, json.RawMessage(emailInput(emailMatter, []string{emailClient}, `,"submit":true`)))
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			g0, err := w.gates.Get(ctx, gate.GateID(res.GateID))
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			d := gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: g0.PayloadHash, MFAVerifiedAt: time.Date(2026, 10, 11, 8, 59, 0, 0, time.UTC)}
			err = agenttools.DecideGuard{Engine: w.gates}.Decide(ctx, nil, g0.ID, g0.Version, d, tc.by)
			g, gerr := w.gates.Get(ctx, g0.ID)
			if gerr != nil {
				t.Fatalf("gate: %v", gerr)
			}
			if tc.refused {
				if !errors.Is(err, agenttools.ErrAgentDecides) || g.Status != gate.StatusPending {
					t.Fatalf("decide err = %v status %s, want ErrAgentDecides and pending", err, g.Status)
				}
				return
			}
			if err != nil || g.Status == gate.StatusPending {
				t.Fatalf("decide err = %v status %s, want decided", err, g.Status)
			}
			if w.drafts.sent != 0 {
				t.Errorf("approval sent through the draft store")
			}
		})
	}
}

// rule: ARC-I-035
// No send path is reachable: the draft store draft_email writes through has no send method.
func TestARC_I_035_DraftEmailHasNoSendPath(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
	}{
		{"email drafts", reflect.TypeFor[agent.EmailDrafts]()},
		{"matter contacts", reflect.TypeFor[agent.MatterContacts]()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for i := range tc.typ.NumMethod() {
				m := strings.ToLower(tc.typ.Method(i).Name)
				for _, verb := range []string{"send", "deliver", "approve", "execute"} {
					if strings.Contains(m, verb) {
						t.Errorf("%s has method %s", tc.typ, tc.typ.Method(i).Name)
					}
				}
			}
		})
	}
}
