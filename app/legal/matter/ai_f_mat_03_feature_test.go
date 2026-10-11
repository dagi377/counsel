package matter_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// --- F-MAT-03 Stage advance watcher

// mat03DraftID is the stage move draft: the seed numbers records in one sequence, the
// evidence document is 1 and the draft 2.
const mat03DraftID = "draft-document-2"

// mat03Script is one F-MAT-03 run: plan, read the matter and the signed agreement, draft the
// stage move proposal and submit it to G-ST, then the extra calls.
func mat03Script(mid, docID, ver string, extra ...matbCall) []matbTurn {
	turns := []matbTurn{
		{Calls: []matbCall{matbPlan("read", "evidence", "draft", "submit")}},
		{Calls: []matbCall{{Name: agent.ToolGetMatter, Input: map[string]any{"matter_id": mid}}}},
		{Calls: []matbCall{{Name: agent.ToolSearchDocuments, Input: map[string]any{"matter_id": mid, "query": "signed"}}}},
		{Calls: []matbCall{{Name: agent.ToolReadDocument, Input: map[string]any{"doc_id": docID, "version_id": ver}}}},
		{Calls: []matbCall{{Name: agent.ToolSearchContacts, Input: map[string]any{"query": "Synth"}}}},
		{Calls: []matbCall{{Name: agent.ToolDraftDocument, Input: map[string]any{
			"matter_id": mid, "title": "Stage move: intake to retained",
			"body_markdown": "The retainer is signed. Propose moving the matter from intake to retained.",
			"citations":     []map[string]any{{"anchor": "retainer", "citation": matbCite(docID, ver)}},
		}}}},
		{Calls: []matbCall{matbSubmit(matter.GateStageMove, mat03DraftID)}},
	}
	for _, c := range extra {
		turns = append(turns, matbTurn{Calls: []matbCall{c}})
	}
	return append(turns, matbTurn{Text: "Proposed the stage move for approval."})
}

// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
// rule: AI-R-003
// rule: AI-R-005
func TestFMAT03_FromFeature(t *testing.T) {
	full := []string{"report_plan", "get_matter", "search_documents", "read_document", "search_contacts", "draft_document", "submit_for_approval"}
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
		{name: "kind outside the Def", extra: []matbCall{matbSubmit("trust.withdrawal", mat03DraftID)},
			wantTools:  append(slices.Clone(full), "submit_for_approval"),
			wantRefuse: map[string]error{"submit_for_approval": agenttools.ErrGateKindNotAllowed}},
		// A subject the run did not draft cannot be submitted.
		{name: "subject not drafted", extra: []matbCall{matbSubmit(matter.GateStageMove, "draft-document-9")},
			wantTools:  append(slices.Clone(full), "submit_for_approval"),
			wantRefuse: map[string]error{"submit_for_approval": agenttools.ErrNotFound}},
		// rule: ARC-I-038
		{name: "injection", inject: true, wantTools: full},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			def := matter.StageAdvanceDef()
			fd := matbFeature(t, def)
			lines := []string{"RETAINER AGREEMENT signed by the client on 2026-10-02.", "The firm is retained for the claim."}
			if c.inject {
				lines = append(lines, matbInjected)
			}
			mid, docID, ver := matbSeed(fd, "2026-0301", "Synth retainer", lines...)
			fd.Seed.Contact("Synth Client")
			run := fd.Fire(context.Background(), matbScript(t, def.ID, mat03Script(mid, docID, ver, c.extra...)...),
				"run-fmat03-"+strings.ReplaceAll(c.name, " ", "-"), "the retainer on the matter was signed")
			matbCheckRun(t, run, c.wantTools, agent.ToolDraftDocument, c.wantRefuse)
			// rule: ARC-I-039
			// The tool-gap kind opens only through the run's own draft, prepared by the acting user.
			if got := fd.Seed.Store.Drafts; len(got) != 1 || got[0].ID != mat03DraftID || got[0].PreparedBy != agenttest.FeatureActingFor || got[0].MatterID != mid {
				t.Fatalf("drafts = %+v", got)
			}
			gs := matbGateDecisions(t, fd, run, []string{matter.GateStageMove})
			if g := gs[0]; g.Subject.Kind != matbSubjectDraft || g.Subject.ID != mat03DraftID {
				t.Fatalf("G-ST subject = %+v, want the run's draft", g.Subject)
			}
			if c.inject {
				if err := matbQuotedOnly(run, matbInjected); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(gs[0].Payload), "ignore previous") {
					t.Fatal("injected text in the G-ST payload")
				}
			}
		})
	}
}

// rule: AI-R-002
func TestFMAT03_Def(t *testing.T) {
	sf := matbSpecSync(t, matter.StageAdvanceDef())
	if sf.Outputs != "Stage move proposal for G-ST" {
		t.Fatalf("spec output = %q", sf.Outputs)
	}
}

func TestFMAT03_Prompt(t *testing.T) {
	matbPrompt(t, matter.StageAdvanceDef(), "Stage advance watcher", "Stage move proposal for G-ST.",
		"draft_document", "submit_for_approval", matter.GateStageMove)
}

// F-MAT-03 starts from an evidence event, and on request from the matter stage board.
func TestFMAT03_TriggerBinding(t *testing.T) {
	b, err := matter.StageAdvanceBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	def := matter.StageAdvanceDef()
	tests := []struct {
		field     string
		got, want string
	}{
		{"feature", b.FeatureID, "F-MAT-03"},
		{"events", strings.Join(b.Events, ","), "document.released,document.uploaded,payment.received,deadline.met"},
		{"cadence", string(b.Cadence), string(trigger.CadenceNone)},
		{"acting for", b.ActingFor, ""},
		{"designated", fmt.Sprint(b.Designated), "false"},
		{"on request", strings.Join(def.Actions, ";"), "matter-stage-board > Propose stage advance"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}
