package agent_test

import (
	"context"
	"encoding/json"
	"errors"
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
	noteMatter  = "71111111-1111-4111-8111-111111111111"
	noteWalled  = "72222222-2222-4222-8222-222222222222"
	noteDoc     = "73333333-3333-4333-8333-333333333333"
	noteVersion = "74444444-4444-4444-8444-444444444444"
	noteLawyer  = "user-lawyer-note"
)

type fakeNoteDrafts struct {
	mu    sync.Mutex
	notes []agent.NewDraftNote
}

func (f *fakeNoteDrafts) CreateNoteDraft(_ context.Context, n agent.NewDraftNote) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notes = append(f.notes, n)
	return "note-draft-" + string(rune('0'+len(f.notes))), nil
}

type noteWorld struct {
	reg   *agenttools.Registry
	notes *fakeNoteDrafts
	gates *gatefake.Fake
}

// newNoteWorld registers the real draft_note on a Registry over module fakes and a fake gate
// engine. Run "run-note" has a plan; "run-noplan" has none. clerk holds actorCaps and is
// walled from noteWalled; noteLawyer may approve notes.
func newNoteWorld(t *testing.T, actorCaps map[string]bool) noteWorld {
	t.Helper()
	ctx := context.Background()
	walls := rolesfake.New()
	walls.Wall(clerk, noteWalled)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	w := noteWorld{
		notes: &fakeNoteDrafts{},
		gates: gatefake.New(func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }, gatefake.Caps{noteLawyer: {"matters.edit"}, clerk: {"matters.edit"}}),
	}
	err := w.gates.Register(gate.GateKind{
		Slug: agent.GateAgentNote, Approver: "matters.edit", Risk: gate.RiskMedium, MinApprovers: 1, TTL: 336 * time.Hour,
		Effect: func(context.Context, pgx.Tx, gate.Approved) error { return nil },
	})
	if err != nil {
		t.Fatalf("gate kind: %v", err)
	}
	open := agenttools.GateOpener(func(ctx context.Context, kind string, s registry.Subject, payload json.RawMessage, by agenttools.Principal) (gate.GateID, error) {
		return w.gates.Open(ctx, nil, kind, s, payload, by)
	})
	tool, err := agent.DraftNoteHandler().Build(agent.NewDeps(can, walls, w.notes, open))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(ctx, "run-note", []string{"note the call"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	w.reg = agenttools.NewRegistry(plans)
	if err := w.reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return w
}

func noteInput(matterID, extra string) string {
	return `{"matter_id":"` + matterID + `","body_markdown":"Synthetic call note"` + extra + `}`
}

func noteCite(page, from, to int) string {
	b, _ := json.Marshal([]map[string]any{{
		"doc_id": noteDoc, "version_id": noteVersion, "page": page, "line_start": from, "line_end": to,
		"quote_hash": "sha256:" + strings.Repeat("cd", 32),
	}})
	return `,"citations":` + string(b)
}

// rule: ARC-I-035
// rule: ARC-I-036
// rule: ARC-I-039
func TestARC_I_035_DraftNote(t *testing.T) {
	edit := map[string]bool{"matters.edit": true}
	grant := []string{"matters.edit"}
	agentP := func(run string, g []string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: run, ActingFor: clerk, Grant: g}
	}
	cases := []struct {
		name      string
		p         registry.Principal
		actorCaps map[string]bool
		in        string
		wantErr   error
		wantCites int
	}{
		{"agent drafts a note", agentP("run-note", grant), edit, noteInput(noteMatter, ""), nil, 0},
		{"agent drafts a cited note", agentP("run-note", grant), edit, noteInput(noteMatter, noteCite(1, 2, 5)), nil, 1},
		{"manual path", registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}, edit, noteInput(noteMatter, ""), nil, 0},
		{"citation with reversed lines", agentP("run-note", grant), edit, noteInput(noteMatter, noteCite(1, 5, 2)), agenttools.ErrCitation, 0},
		{"walled matter", agentP("run-note", grant), edit, noteInput(noteWalled, ""), agent.ErrNotVisible, 0},
		{"walled matter on the manual path", registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}, edit, noteInput(noteWalled, ""), agent.ErrNotVisible, 0},
		{"run without the capability", agentP("run-note", nil), edit, noteInput(noteMatter, ""), agenttools.ErrMissingCapability, 0},
		{"acting user without the capability", agentP("run-note", grant), map[string]bool{}, noteInput(noteMatter, ""), agent.ErrForbidden, 0},
		{"no report_plan in the run", agentP("run-noplan", grant), edit, noteInput(noteMatter, ""), agenttools.ErrPlanRequired, 0},
		{"body missing", agentP("run-note", grant), edit, `{"matter_id":"` + noteMatter + `"}`, agenttools.ErrInvalidInput, 0},
		{"unknown field", agentP("run-note", grant), edit, noteInput(noteMatter, `,"approve":true`), agenttools.ErrInvalidInput, 0},
		{"citation without quote hash", agentP("run-note", grant), edit, noteInput(noteMatter, `,"citations":[{"doc_id":"`+noteDoc+`","version_id":"`+noteVersion+`","page":1,"line_start":1,"line_end":1}]`), agenttools.ErrInvalidInput, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newNoteWorld(t, tc.actorCaps)
			res, err := w.reg.Call(ctx, tc.p, agent.ToolDraftNote, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				if len(w.notes.notes) != 0 {
					t.Errorf("refused call stored %d notes", len(w.notes.notes))
				}
				return
			}
			// rule: ARC-I-035
			if err := agenttools.CheckWriteResult(res); err != nil {
				t.Fatalf("write result: %v", err)
			}
			if err := agenttools.CheckCitations(res); err != nil {
				t.Fatalf("citations: %v", err)
			}
			if len(w.notes.notes) != 1 || w.notes.notes[0].PreparedBy != clerk || len(w.notes.notes[0].Citations) != tc.wantCites {
				t.Fatalf("notes = %+v, want one prepared by the acting user", w.notes.notes)
			}
			g, err := w.gates.Get(ctx, gate.GateID(res.GateID))
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			// rule: ARC-I-039
			// The gate is pending with the acting user as preparer.
			if g.Kind != agent.GateAgentNote || g.Status != gate.StatusPending ||
				g.Subject != (registry.Subject{Kind: agent.SubjectAgentNote, ID: "note-draft-1"}) {
				t.Errorf("gate = %+v", g)
			}
			preparer := g.Preparer.UserID
			if g.Preparer.Kind == registry.PrincipalAgent {
				preparer = g.Preparer.ActingFor
			}
			if preparer != clerk {
				t.Errorf("preparer = %+v, want acting user %s", g.Preparer, clerk)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(g.Payload, &payload); err != nil {
				t.Fatalf("payload: %v", err)
			}
			if _, has := payload["citations"]; has != (tc.wantCites > 0) {
				t.Errorf("payload citations present = %v, want %v", has, tc.wantCites > 0)
			}
		})
	}
}

// rule: ARC-I-039
// The note's gate is decided by a person other than the preparer: an agent principal is
// refused and the gate stays pending.
func TestARC_I_039_DraftNoteGateNotDecidedByAgent(t *testing.T) {
	agentP := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-note", ActingFor: clerk, Grant: []string{"matters.edit"}}
	cases := []struct {
		name    string
		by      registry.Principal
		refused bool
	}{
		{"agent run approves its note", agentP, true},
		{"agent acting for the lawyer approves", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-y", ActingFor: noteLawyer, Grant: []string{"matters.edit"}}, true},
		{"lawyer approves", registry.Principal{Kind: registry.PrincipalUser, UserID: noteLawyer}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newNoteWorld(t, map[string]bool{"matters.edit": true})
			res, err := w.reg.Call(ctx, agentP, agent.ToolDraftNote, json.RawMessage(noteInput(noteMatter, "")))
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
		})
	}
}
