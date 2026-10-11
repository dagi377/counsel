package matter_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/conflict/conflictfake"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// mat04CheckID is the synthetic conflict check the run proposes the matter's parties for.
const mat04CheckID = "chk-synth-mat04"

// mat04Checks holds the matter's conflict check and the parties the run proposed on it
// (agent.ConflictCheckParties), over a conflict checker fake. Every record is synthetic.
type mat04Checks struct {
	mu       sync.Mutex
	Checker  *conflictfake.Fake
	checks   map[string]string // check id -> matter id
	Proposed map[string][]agent.ProposedParty
}

func newMat04Checks() *mat04Checks {
	return &mat04Checks{
		Checker: conflictfake.New(conflictfake.Party{ID: "pty-1", Name: "Synth Freight Ltd", MatterID: "matter-old", Role: "adverse"}),
		checks:  map[string]string{}, Proposed: map[string][]agent.ProposedParty{},
	}
}

// CheckMatter implements agent.ConflictCheckParties.
func (c *mat04Checks) CheckMatter(_ context.Context, checkID string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.checks[checkID]
	if !ok {
		return "", conflict.ErrNotFound
	}
	return m, nil
}

// ProposeParties implements agent.ConflictCheckParties: the party names are the check terms.
func (c *mat04Checks) ProposeParties(_ context.Context, checkID string, ps []agent.ProposedParty, _ string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Proposed[checkID] = append(c.Proposed[checkID], ps...)
	terms := make([]string, 0, len(ps))
	for _, p := range ps {
		terms = append(terms, p.Name)
	}
	return terms, nil
}

// mat04Decisions checks G2 for the matter's check. propose_conflict_parties opens no kernel
// gate: the open check run is the pending matter.party_conflict (docs/QUESTIONS.md
// Q-FEAT-CON-01). It is open with the acting user as preparer; an agent principal deciding
// is 403; the acting user deciding their own check is the self-approval refusal; it stays open.
func mat04Decisions(t *testing.T, c *mat04Checks, mid string) {
	t.Helper()
	ctx := context.Background()
	runs, err := c.Checker.Runs(ctx, mid)
	if err != nil || len(runs) != 1 {
		t.Fatalf("check runs = %v (%v), want one", runs, err)
	}
	r := runs[0]
	// rule: ARC-I-039
	if r.Status != conflict.StatusOpen || r.PreparedBy != agenttest.FeatureActingFor {
		t.Fatalf("check run = %s prepared by %q, want open, prepared by the acting user", r.Status, r.PreparedBy)
	}
	gf, err := gatesfake.New(gates.Kind{Code: matter.GatePartyConflict, Approver: "conflicts.clear"})
	if err != nil {
		t.Fatal(err)
	}
	g := gates.Gate{ID: r.ID, Code: matter.GatePartyConflict, Preparer: gates.Principal{UserID: r.PreparedBy}, OpenedAt: r.RanAt}
	caps := []string{"conflicts.clear"}
	cases := []struct {
		name string
		by   gates.Principal
		want func(error) bool
	}{
		// rule: AI-R-003
		{"agent principal -> 403", gates.Principal{UserID: "agent", Agent: true, ActingFor: "user-synthetic-partner", Capabilities: caps},
			func(err error) bool { return agenttest.DecisionStatus(err) == http.StatusForbidden }},
		{"acting user -> self-approval refused", gates.Principal{UserID: agenttest.FeatureActingFor, Capabilities: caps},
			func(err error) bool { return errors.Is(err, gates.ErrPreparerApproves) }},
	}
	for _, tc := range cases {
		if err := gf.CheckDecision(ctx, g, tc.by, r.RanAt.Add(time.Hour)); !tc.want(err) {
			t.Fatalf("%s: err = %v", tc.name, err)
		}
	}
	if _, err := c.Checker.Decide(ctx, r.ID, conflict.Actor{UserID: "agent", IsAgent: true, ActingFor: "user-synthetic-partner", IsLawyer: true}, true, ""); !errors.Is(err, conflict.ErrAgentApproves) {
		t.Fatalf("agent decides the check: %v", err)
	}
	if _, err := c.Checker.Decide(ctx, r.ID, conflict.Actor{UserID: agenttest.FeatureActingFor, IsLawyer: true}, true, ""); !errors.Is(err, conflict.ErrPreparerApproves) {
		t.Fatalf("acting user decides own check: %v", err)
	}
	if runs, _ := c.Checker.Runs(ctx, mid); runs[0].Status != conflict.StatusOpen {
		t.Fatalf("check decided: %s", runs[0].Status)
	}
}

// mat04Script is one F-MAT-04 run: plan, read the matter and the filed pleading, propose
// its parties with their roles, then the extra calls.
func mat04Script(mid, docID, ver string, extra ...matbCall) []matbTurn {
	turns := []matbTurn{
		{Calls: []matbCall{matbPlan("read", "parties", "propose")}},
		{Calls: []matbCall{{Name: agent.ToolGetMatter, Input: map[string]any{"matter_id": mid}}}},
		{Calls: []matbCall{{Name: agent.ToolSearchDocuments, Input: map[string]any{"matter_id": mid, "query": "statement of claim"}}}},
		{Calls: []matbCall{{Name: agent.ToolReadDocument, Input: map[string]any{"doc_id": docID, "version_id": ver}}}},
		{Calls: []matbCall{{Name: agent.ToolSearchContacts, Input: map[string]any{"query": "Synth Freight"}}}},
		{Calls: []matbCall{{Name: agent.ToolProposeConflictParties, Input: map[string]any{"check_id": mat04CheckID, "parties": []map[string]any{
			{"name": "Synth Freight Ltd", "role": "adverse", "kind": "org", "source": matbCite(docID, ver)},
			{"name": "Robin Synth", "role": "witness", "kind": "person", "source": matbCite(docID, ver)},
		}}}}},
	}
	for _, c := range extra {
		turns = append(turns, matbTurn{Calls: []matbCall{c}})
	}
	return append(turns, matbTurn{Text: "Proposed two parties for the conflict check."})
}

// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
// rule: AI-R-003
// rule: AI-R-005
func TestFMAT04_FromFeature(t *testing.T) {
	full := []string{"report_plan", "get_matter", "search_documents", "read_document", "search_contacts", "propose_conflict_parties"}
	cases := []struct {
		name       string
		inject     bool
		extra      []matbCall
		wantTools  []string
		wantRefuse map[string]error
	}{
		{name: "ok", wantTools: full},
		// rule: AI-R-003
		// A gate kind outside the Def is refused before anything opens.
		{name: "kind outside the Def", extra: []matbCall{matbSubmit("trust.withdrawal", mat04CheckID)},
			wantTools:  append(slices.Clone(full), "submit_for_approval"),
			wantRefuse: map[string]error{"submit_for_approval": agenttools.ErrGateKindNotAllowed}},
		// rule: ARC-I-038
		{name: "injection", inject: true, wantTools: full},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			def := matter.PartyExtractionDef()
			checks := newMat04Checks()
			fd := matbFeature(t, def, checks, conflict.Checker(checks.Checker))
			lines := []string{"STATEMENT OF CLAIM. The plaintiff sues Synth Freight Ltd for the lost cargo.", "Robin Synth saw the cargo loaded."}
			if c.inject {
				lines = append(lines, matbInjected)
			}
			mid, docID, ver := matbSeed(fd, "2026-0401", "Synth cargo claim", lines...)
			checks.mu.Lock()
			checks.checks[mat04CheckID] = mid
			checks.mu.Unlock()
			run := fd.Fire(context.Background(), matbScript(t, def.ID, mat04Script(mid, docID, ver, c.extra...)...),
				"run-fmat04-"+strings.ReplaceAll(c.name, " ", "-"), "a statement of claim was filed on the matter")
			matbCheckRun(t, run, c.wantTools, agent.ToolProposeConflictParties, c.wantRefuse)
			if len(run.GateIDs) != 0 {
				t.Fatalf("kernel gates = %v, want none: G2 is the open check", run.GateIDs)
			}
			got := checks.Proposed[mat04CheckID]
			var names []string
			for _, p := range got {
				names = append(names, p.Name+"/"+p.Role)
				if p.Source == nil || p.Source.VersionID != ver {
					t.Fatalf("party %s source = %+v, want the filed document", p.Name, p.Source)
				}
			}
			if strings.Join(names, ",") != "Synth Freight Ltd/adverse,Robin Synth/witness" {
				t.Fatalf("proposed = %v", names)
			}
			mat04Decisions(t, checks, mid)
			if c.inject {
				if err := matbQuotedOnly(run, matbInjected); err != nil {
					t.Fatal(err)
				}
				for _, p := range got {
					if strings.Contains(p.Name+p.Role, "ignore previous") {
						t.Fatalf("injected text in proposed party %+v", p)
					}
				}
			}
		})
	}
}

// rule: AI-R-002
func TestFMAT04_Def(t *testing.T) {
	sf := matbSpecSync(t, matter.PartyExtractionDef())
	if sf.Outputs != "Party and role proposals for G2" {
		t.Fatalf("spec output = %q", sf.Outputs)
	}
}

func TestFMAT04_Prompt(t *testing.T) {
	matbPrompt(t, matter.PartyExtractionDef(), "Party extraction", "Party and role proposals for G2.",
		"propose_conflict_parties", matter.GatePartyConflict)
}

// F-MAT-04 starts from a filed document, and on request from the matter parties screen.
func TestFMAT04_TriggerBinding(t *testing.T) {
	b, err := matter.PartyExtractionBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	def := matter.PartyExtractionDef()
	tests := []struct {
		field     string
		got, want string
	}{
		{"feature", b.FeatureID, "F-MAT-04"},
		{"events", strings.Join(b.Events, ","), "document.uploaded"},
		{"cadence", string(b.Cadence), string(trigger.CadenceNone)},
		{"acting for", b.ActingFor, ""},
		{"on request", strings.Join(def.Actions, ";"), "matter-parties > Propose party from document or email"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}
