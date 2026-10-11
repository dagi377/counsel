package readiness_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/schemas"
	"github.com/dagi377/counsel/app/legal/readiness"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/registry"
)

// rdy09Doc is the synthetic research document: only its first line holds the query.
var rdy09Doc = []string{"Hearing set for 2026-11-02 before the motions judge.", "Motion record served 2026-10-01 on all parties."}

// rdy09Ask runs F-RDY-09 with grant over a seeded matter whose one document is lines: one
// search_documents call, then a JSON answer citing page and line of the seeded version.
func rdy09Ask(t *testing.T, grant []string, lines []string, answer string, page, line int) (rdycWorld, agenttest.FeatureRun) {
	t.Helper()
	def := readiness.AskDef()
	if grant != nil {
		def.Grant = grant
	}
	w := rdycFeature(agenttest.FromFeature(t, def), lines...)
	ans, err := json.Marshal(map[string]any{
		"answer":    answer,
		"citations": []map[string]any{{"version_id": w.version, "page": page, "from_line": line, "to_line": line}},
	})
	if err != nil {
		t.Fatal(err)
	}
	model := rdycScript("F-RDY-09", []rdycCall{rdycSearch(w, "hearing")}, string(ans))
	r := w.fd.Fire(context.Background(), model, "run-frdy09", "screen: research\nrecords: matter:"+w.matterID+"\nWhen is the hearing?")
	if r.Err != nil {
		t.Fatalf("run: %v", r.Err)
	}
	return w, r
}

// rdy09Outcome is the run as the screen-action route sees it: the final answer and every
// citation the run's search_documents quotes carried.
func rdy09Outcome(r agenttest.FeatureRun) run.Outcome {
	out := run.Outcome{Text: r.Text, JSON: r.JSON, GateIDs: r.GateIDs}
	for _, b := range r.ToolResults {
		var res struct {
			Quotes []agenttools.Quoted `json:"quotes"`
		}
		if b.IsError || json.Unmarshal([]byte(b.Text), &res) != nil {
			continue
		}
		for _, q := range res.Quotes {
			if q.Untrusted {
				out.Citations = append(out.Citations, q.Source)
			}
		}
	}
	return out
}

// rule: AI-R-003
func TestFRDY09_DefMatchesSpec(t *testing.T) {
	rdycCheckDefMatchesSpec(t, readiness.AskDef())
}

// rule: AI-R-005
func TestFRDY09_Prompt(t *testing.T) {
	rdycCheckPrompt(t, readiness.AskDef(), "Your task: answer the user's question", "ask on research (Answer with citations)",
		"read-only", "open no gate", `{"answer": "...", "citations": [...]}`)
}

// F-RDY-09 runs on request from the research screen's Ask action.
func TestFRDY09_TriggerBinding(t *testing.T) {
	rdycCheckAction(t, readiness.AskDef(), readiness.ActionAsk)
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
// search_documents runs its real handler; the answer passes CheckAnswer only when it cites a
// line the search returned; the injected line reaches the model as quoted data only; without
// documents.view the search is refused and nothing is cited. No run writes or opens a gate.
func TestFRDY09_FromFeature(t *testing.T) {
	// rule: AI-R-003
	// The Def has no write tool and no gate kind: the agent only reads and answers.
	for _, name := range readiness.AskDef().Tools {
		s, err := schemas.ByName(name)
		if err != nil || s.Kind != registry.ToolRead {
			t.Fatalf("tool %s: kind %v (%v), want read", name, s.Kind, err)
		}
	}
	if k := readiness.AskDef().GateKinds; len(k) != 0 {
		t.Fatalf("gate kinds = %v, want none", k)
	}
	injected := append(slices.Clone(rdy09Doc), "Hearing note: "+rdycInjected+".")
	tests := []struct {
		name        string
		grant       []string
		doc         []string
		page, line  int
		wantRefused error // the refusal of search_documents
		wantAnswer  error // run.CheckAnswer
		injected    bool
	}{
		{name: "ok", doc: rdy09Doc, page: 1, line: 1},
		{name: "injection quoted only", doc: injected, page: 1, line: 1, injected: true},
		{name: "citation not returned", doc: rdy09Doc, page: 1, line: 2, wantAnswer: run.ErrUncitedAnswer},
		{name: "403 without documents.view", grant: []string{"agent.ask"}, doc: rdy09Doc, page: 1, line: 1,
			wantRefused: agenttools.ErrMissingCapability, wantAnswer: run.ErrUncitedAnswer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, r := rdy09Ask(t, tt.grant, tt.doc, "From the search: the hearing is set for 2026-11-02.", tt.page, tt.line)
			// rule: ARC-I-038
			// The script's one call ran on the real handler and the run made no other call.
			if !slices.Equal(r.Tools, []string{agent.ToolSearchDocuments}) || len(w.fd.StandIns) != 0 {
				t.Fatalf("tools = %v, stand-ins %v; want one real search_documents", r.Tools, w.fd.StandIns)
			}
			if tt.wantRefused != nil {
				if len(r.Refused) != 1 || !errors.Is(r.Refused[0].Err, tt.wantRefused) {
					t.Fatalf("refused = %v, want search_documents refused with %v", r.Refused, tt.wantRefused)
				}
			} else if len(r.Refused) != 0 {
				t.Fatalf("refused = %v, want none", r.Refused)
			}
			// rule: AI-R-005
			// No write, no gate row: nothing is stored or proposed.
			if r.FirstWrite >= 0 || len(r.GateIDs) != 0 || len(w.fd.Seed.Store.Drafts) != 0 {
				t.Fatalf("write at %d, gates %v, drafts %v; want none", r.FirstWrite, r.GateIDs, w.fd.Seed.Store.Drafts)
			}
			spec, err := readiness.AskDef().Spec()
			if err != nil {
				t.Fatalf("Spec: %v", err)
			}
			ans, err := run.CheckAnswer(spec, rdy09Outcome(r))
			if !errors.Is(err, tt.wantAnswer) {
				t.Fatalf("CheckAnswer = %v, want %v", err, tt.wantAnswer)
			}
			if err == nil && (len(ans.Citations) != 1 || ans.Citations[0].VersionID != w.version) {
				t.Fatalf("citations = %v, want the seeded version %s", ans.Citations, w.version)
			}
			if tt.injected {
				if err := agenttest.QuotedOnly(r.Run, rdycInjected); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// rule: ARC-I-039
// A read-only run leaves nothing for anyone to decide: a decision on any gate id, by the
// acting user or anyone else, is refused as unknown.
func TestFRDY09_AgentNeverApproves(t *testing.T) {
	w, r := rdy09Ask(t, nil, rdy09Doc, "From the search: the hearing is set for 2026-11-02.", 1, 1)
	if len(r.GateIDs) != 0 {
		t.Fatalf("gates = %v, want none", r.GateIDs)
	}
	for _, by := range []string{agenttest.FeatureActingFor, rdycOtherLawyer} {
		_, err := w.fd.Decided(context.Background(), agent.GateDecided{GateID: "gate-any", Decision: agent.Approved, By: by})
		if !errors.Is(err, agent.ErrUnknownGate) {
			t.Fatalf("Decided by %s = %v, want ErrUnknownGate", by, err)
		}
	}
}
