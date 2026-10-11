package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/conflict/conflictfake"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	cpMatter    = "a1111111-1111-4111-8111-111111111111"
	cpWalled    = "a2222222-2222-4222-8222-222222222222"
	cpCheck     = "a3333333-3333-4333-8333-333333333333"
	cpWallCheck = "a4444444-4444-4444-8444-444444444444"
	cpLeadCheck = "a5555555-5555-4555-8555-555555555555"
	cpUnknown   = "a6666666-6666-4666-8666-666666666666"
	cpDoc       = "a7777777-7777-4777-8777-777777777777"
	cpVersion   = "a8888888-8888-4888-8888-888888888888"
	cpLawyer    = "user-lawyer-conflict"
	cpRun       = "run-conflict"
	cpNoPlan    = "run-conflict-noplan"
	cpCap       = "conflicts.run"
)

var errNoCheck = errors.New("test: no such check")

// fakeCheckParties keeps proposed parties per check. cpCheck is on cpMatter, cpWallCheck on
// cpWalled and cpLeadCheck on a lead only.
type fakeCheckParties struct {
	mu      sync.Mutex
	parties map[string][]agent.ProposedParty
	by      []string
}

func (f *fakeCheckParties) CheckMatter(_ context.Context, checkID string) (string, error) {
	switch checkID {
	case cpCheck:
		return cpMatter, nil
	case cpWallCheck:
		return cpWalled, nil
	case cpLeadCheck:
		return "", nil
	}
	return "", errNoCheck
}

func (f *fakeCheckParties) ProposeParties(_ context.Context, checkID string, ps []agent.ProposedParty, by string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.parties[checkID] = append(f.parties[checkID], ps...)
	f.by = append(f.by, by)
	terms := make([]string, 0, len(f.parties[checkID]))
	for _, p := range f.parties[checkID] {
		terms = append(terms, p.Name)
	}
	return terms, nil
}

type conflictWorld struct {
	reg     *agenttools.Registry
	parties *fakeCheckParties
	checks  *conflictfake.Fake
}

// newConflictWorld registers the real propose_conflict_parties on a Registry over module
// fakes. cpRun has a plan; cpNoPlan has none. clerk holds actorCaps and is walled from cpWalled.
func newConflictWorld(t *testing.T, actorCaps map[string]bool) conflictWorld {
	t.Helper()
	ctx := context.Background()
	walls := rolesfake.New()
	walls.Wall(clerk, cpWalled)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	w := conflictWorld{
		parties: &fakeCheckParties{parties: map[string][]agent.ProposedParty{}},
		checks:  conflictfake.New(conflictfake.Party{ID: "party-1", Name: "Synth Holdings Inc.", MatterID: "m-old", Role: "adverse"}),
	}
	var checker conflict.Checker = w.checks
	tool, err := agent.ProposeConflictPartiesHandler().Build(agent.NewDeps(can, walls, w.parties, checker, agenttools.RunScopes{}))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(ctx, cpRun, []string{"propose parties"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	w.reg = agenttools.NewRegistry(plans)
	if err := w.reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return w
}

func cpInput(checkID string, parties ...string) string {
	return `{"check_id":"` + checkID + `","parties":[` + strings.Join(parties, ",") + `]}`
}

func cpParty(name, extra string) string {
	return `{"name":"` + name + `","role":"adverse"` + extra + `}`
}

func cpSource(from, to int) string {
	b, _ := json.Marshal(map[string]any{
		"doc_id": cpDoc, "version_id": cpVersion, "page": 1, "line_start": from, "line_end": to,
		"quote_hash": "sha256:" + strings.Repeat("ab", 32),
	})
	return `,"kind":"org","source":` + string(b)
}

// cpRead is a citation of lines from..to on page 1 of cpVersion, as read_document returns it.
func cpRead(from, to int) *agenttools.Citation {
	return &agenttools.Citation{VersionID: cpVersion, Page: 1, FromLine: from, ToLine: to}
}

// rule: ARC-I-035
// rule: ARC-I-036
// rule: ARC-I-039
// rule: ARC-U-027
func TestARC_I_035_ProposeConflictParties(t *testing.T) {
	run := map[string]bool{cpCap: true}
	grant := []string{cpCap}
	agentP := func(r string, g []string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: r, ActingFor: clerk, Grant: g}
	}
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}
	one := cpInput(cpCheck, cpParty("Synth Holdings", ""))
	cases := []struct {
		name      string
		p         registry.Principal
		actorCaps map[string]bool
		in        string
		wantErr   error
		wantMat   string
		wantHits  int
		wantCites int
		// read is the citation a tool result returned to the run before the call, if any.
		read *agenttools.Citation
	}{
		{"agent proposes a party with a hit", agentP(cpRun, grant), run, one, nil, cpMatter, 1, 0, nil},
		{"agent proposes cited parties", agentP(cpRun, grant), run, cpInput(cpCheck, cpParty("Avery Synth", cpSource(2, 3)), cpParty("Synth Holdings", "")), nil, cpMatter, 1, 1, cpRead(1, 4)},
		// rule: ARC-U-027
		{"agent cites a passage the run never read", agentP(cpRun, grant), run, cpInput(cpCheck, cpParty("Avery Synth", cpSource(2, 3))), agenttools.ErrUnsourcedValue, "", 0, 0, nil},
		{"agent cites past the lines the run read", agentP(cpRun, grant), run, cpInput(cpCheck, cpParty("Avery Synth", cpSource(2, 6))), agenttools.ErrUnsourcedValue, "", 0, 0, cpRead(1, 4)},
		{"cited lines on another page", agentP(cpRun, grant), run, cpInput(cpCheck, cpParty("Avery Synth", cpSource(2, 3))), agenttools.ErrUnsourcedValue, "", 0, 0, &agenttools.Citation{VersionID: cpVersion, Page: 2, FromLine: 1, ToLine: 9}},
		{"manual path cites without a read", user, run, cpInput(cpCheck, cpParty("Avery Synth", cpSource(2, 3))), nil, cpMatter, 0, 1, nil},
		{"check on a lead only", agentP(cpRun, grant), run, cpInput(cpLeadCheck, cpParty("Avery Synth", "")), nil, "", 0, 0, nil},
		{"manual path", user, run, one, nil, cpMatter, 1, 0, nil},
		{"walled matter", agentP(cpRun, grant), run, cpInput(cpWallCheck, cpParty("Avery Synth", "")), agent.ErrNotVisible, "", 0, 0, nil},
		{"walled matter on the manual path", user, run, cpInput(cpWallCheck, cpParty("Avery Synth", "")), agent.ErrNotVisible, "", 0, 0, nil},
		{"unknown check", agentP(cpRun, grant), run, cpInput(cpUnknown, cpParty("Avery Synth", "")), errNoCheck, "", 0, 0, nil},
		{"run without the capability", agentP(cpRun, nil), run, one, agenttools.ErrMissingCapability, "", 0, 0, nil},
		{"acting user without the capability", agentP(cpRun, grant), map[string]bool{}, one, agent.ErrForbidden, "", 0, 0, nil},
		{"no report_plan in the run", agentP(cpNoPlan, grant), run, one, agenttools.ErrPlanRequired, "", 0, 0, nil},
		{"citation with reversed lines", agentP(cpRun, grant), run, cpInput(cpCheck, cpParty("Avery Synth", cpSource(5, 2))), agenttools.ErrCitation, "", 0, 0, nil},
		{"no parties", agentP(cpRun, grant), run, cpInput(cpCheck), agenttools.ErrInvalidInput, "", 0, 0, nil},
		{"party without a role", agentP(cpRun, grant), run, cpInput(cpCheck, `{"name":"Avery Synth"}`), agenttools.ErrInvalidInput, "", 0, 0, nil},
		{"clearance in the input", agentP(cpRun, grant), run, `{"check_id":"` + cpCheck + `","parties":[` + cpParty("x", "") + `],"clear":true}`, agenttools.ErrInvalidInput, "", 0, 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newConflictWorld(t, tc.actorCaps)
			// The run's scope holds the citations its tool results returned.
			scope := agenttools.NewScope(tc.p.RunID, []string{conflict.GateClearance})
			if tc.read != nil {
				scope.AddCitations([]agenttools.Citation{*tc.read})
			}
			ctx = agenttools.WithScope(ctx, scope)
			res, err := w.reg.Call(ctx, tc.p, agent.ToolProposeConflictParties, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				if len(w.parties.by) != 0 {
					t.Errorf("refused call proposed parties %d times", len(w.parties.by))
				}
				return
			}
			// rule: ARC-I-035
			if err := agenttools.CheckWriteResult(res); err != nil {
				t.Fatalf("write result: %v", err)
			}
			if len(res.Citations) != tc.wantCites {
				t.Errorf("citations = %d, want %d", len(res.Citations), tc.wantCites)
			}
			var out agenttools.WriteOutput
			if err := json.Unmarshal(res.Output, &out); err != nil || out.Outcome != agenttools.OutcomeProposal {
				t.Fatalf("output = %s, want a proposal", res.Output)
			}
			runs, err := w.checks.Runs(ctx, tc.wantMat)
			if err != nil || len(runs) != 1 {
				t.Fatalf("runs = %v err %v, want one", runs, err)
			}
			r := runs[0]
			// The run is open (clearance pending) and prepared by the acting user.
			if r.ID != out.Ref || r.Status != conflict.StatusOpen || r.PreparedBy != clerk || len(r.Hits) != tc.wantHits {
				t.Errorf("run = %+v", r)
			}
			if len(w.parties.by) != 1 || w.parties.by[0] != clerk {
				t.Errorf("proposed by %v, want %s", w.parties.by, clerk)
			}
		})
	}
}

// rule: ARC-I-039
// Clearance (G2) is never decided by the agent or the user it acted for; it stays pending.
func TestARC_I_039_ProposeConflictPartiesClearanceNotDecidedByAgent(t *testing.T) {
	cases := []struct {
		name    string
		by      conflict.Actor
		wantErr error
	}{
		{"agent clears", conflict.Actor{UserID: clerk, IsAgent: true, ActingFor: clerk, IsLawyer: true}, conflict.ErrAgentApproves},
		{"agent acting for a lawyer clears", conflict.Actor{UserID: cpLawyer, IsAgent: true, ActingFor: cpLawyer, IsLawyer: true}, conflict.ErrAgentApproves},
		{"acting user clears", conflict.Actor{UserID: clerk, IsLawyer: true}, conflict.ErrPreparerApproves},
		{"another lawyer clears", conflict.Actor{UserID: cpLawyer, IsLawyer: true}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// rule: ARC-I-035
			// The Runner's scope for a run whose feature declares conflict.GateClearance.
			ctx := agenttools.WithScope(context.Background(), agenttools.NewScope(cpRun, []string{conflict.GateClearance}))
			w := newConflictWorld(t, map[string]bool{cpCap: true})
			p := registry.Principal{Kind: registry.PrincipalAgent, RunID: cpRun, ActingFor: clerk, Grant: []string{cpCap}}
			res, err := w.reg.Call(ctx, p, agent.ToolProposeConflictParties, json.RawMessage(cpInput(cpCheck, cpParty("Avery Synth", ""))))
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			var out agenttools.WriteOutput
			if err := json.Unmarshal(res.Output, &out); err != nil {
				t.Fatalf("output: %v", err)
			}
			r, err := w.checks.Decide(ctx, out.Ref, tc.by, true, "no conflict")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("decide err = %v, want %v", err, tc.wantErr)
			}
			if wantOpen := tc.wantErr != nil; (r.Status == conflict.StatusOpen) != wantOpen {
				t.Errorf("status = %s, want open %v", r.Status, wantOpen)
			}
		})
	}
}
