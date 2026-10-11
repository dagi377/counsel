package conflict_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/conflict"
)

// conRun drives def once over a seeded matter with an intake document and checks the run:
// no stand-ins, report_plan first, propose_conflict_parties the first write and no extra call,
// G2 pending with the acting user as preparer and refused to the agent and the acting user,
// and in the injection run the injected text only as quoted data and in no proposal.
func conRun(t *testing.T, def feature.Def, inject bool) {
	t.Helper()
	ctx := context.Background()
	fd, w, mid := conWorldFeature(t, def)
	if len(fd.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, want none", fd.StandIns)
	}
	fd.Seed.Contact("Synth Holdings Ltd")
	lines := []string{"Intake: Synth Holdings Inc. retains the firm.", "Adverse party: Riverbend Bakery Co-op."}
	if inject {
		lines = append(lines, conInjected)
	}
	docID, ver := fd.Seed.Document(mid, lines)
	source := map[string]any{"doc_id": docID, "version_id": string(ver), "page": 1, "line_start": 2, "line_end": 2,
		"quote_hash": "sha256:" + strings.Repeat("ab", 32)}
	model := conTurnScript(t, def.ID,
		conTurn{Calls: []conTurnCall{conPlan("search", "read", "propose")}},
		conTurn{Calls: []conTurnCall{{Name: "search_contacts", Input: map[string]any{"query": "Synth Holdings"}}}},
		conTurn{Calls: []conTurnCall{{Name: "search_matters", Input: map[string]any{"query": "Synth"}}}},
		conTurn{Calls: []conTurnCall{{Name: "read_document", Input: map[string]any{"doc_id": docID, "version_id": string(ver)}}}},
		conTurn{Calls: []conTurnCall{{Name: "propose_conflict_parties", Input: map[string]any{"check_id": conCheckID, "parties": []any{
			map[string]any{"name": "Synth Holdings Inc.", "role": "client", "kind": "org"},
			map[string]any{"name": "Riverbend Bakery Co-op", "role": "adverse", "kind": "org", "source": source},
		}}}}},
		conTurn{Text: "Proposed two parties; the check is waiting for G2."},
	)
	run := fd.Fire(ctx, model, "run-"+strings.ToLower(def.ID), "conflict check "+conCheckID)
	if run.Err != nil {
		t.Fatalf("run: %v", run.Err)
	}
	want := []string{"report_plan", "search_contacts", "search_matters", "read_document", "propose_conflict_parties"}
	if !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v (no extra call)", run.Tools, want)
	}
	if len(run.Refused) != 0 {
		t.Fatalf("refused = %v", run.Refused)
	}
	// rule: ARC-I-039
	if run.PlanAt != 0 || run.FirstWrite != 4 {
		t.Fatalf("plan at %d, first write %d: report_plan must precede propose_conflict_parties", run.PlanAt, run.FirstWrite)
	}
	conGateDecisions(t, w, mid)
	if !inject {
		return
	}
	// rule: ARC-I-038
	if err := agenttest.QuotedOnly(run.Run, conInjected); err != nil {
		t.Fatal(err)
	}
	for _, v := range w.proposedNames() {
		if strings.Contains(v, "ignore previous") {
			t.Fatalf("injected text reached a proposed party")
		}
	}
	for _, req := range run.Requests {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Type == "tool_use" && strings.Contains(string(b.Input), "ignore previous") {
					t.Fatalf("injected text in %s input", b.ToolName)
				}
			}
		}
	}
}

// rule: AI-R-003
// rule: ARC-I-035
func TestFCON02_DefMatchesSpec(t *testing.T) {
	conCheckDefMatchesSpec(t, conflict.SearchRankingDef())
}

// rule: AI-R-005
func TestFCON02_Prompt(t *testing.T) {
	conPromptLints(t, conflict.SearchRankingDef(), "Conflict search and ranking",
		"Hit list with proposed dispositions for conflict-results")
}

func TestFCON02_TriggerBinding(t *testing.T) {
	b, err := conflict.SearchRankingBinding()
	conBindingIs(t, b, err, "F-CON-02", conflict.EventCheckCompleted)
}

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFCON02_FromFeature(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inject bool
	}{{"ok", false}, {"injection", true}} {
		t.Run(tc.name, func(t *testing.T) {
			conRun(t, conflict.SearchRankingDef(), tc.inject)
		})
	}
}
