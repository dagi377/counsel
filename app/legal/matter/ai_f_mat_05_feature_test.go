package matter_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// mat05DraftID is the merge proposal draft: the seed numbers records in one sequence, the
// engagement letter is 1 and the draft 2.
const mat05DraftID = "draft-document-2"

// mat05Script is one F-MAT-05 run: plan, find the duplicate contacts, read the engagement
// letter that shows they are one org, draft the merge proposal and submit it to G-CM, then
// the extra calls.
func mat05Script(mid, docID, ver string, extra ...matbCall) []matbTurn {
	turns := []matbTurn{
		{Calls: []matbCall{matbPlan("find", "evidence", "draft", "submit")}},
		{Calls: []matbCall{{Name: agent.ToolSearchContacts, Input: map[string]any{"query": "Synth Logistics"}}}},
		{Calls: []matbCall{{Name: agent.ToolGetMatter, Input: map[string]any{"matter_id": mid}}}},
		{Calls: []matbCall{{Name: agent.ToolSearchDocuments, Input: map[string]any{"matter_id": mid, "query": "engagement"}}}},
		{Calls: []matbCall{{Name: agent.ToolReadDocument, Input: map[string]any{"doc_id": docID, "version_id": ver}}}},
		{Calls: []matbCall{{Name: agent.ToolDraftDocument, Input: map[string]any{
			"matter_id": mid, "title": "Contact merge: Synth Logistics",
			"body_markdown": "Synth Logistics Inc. and Synth Logistics are one org (same address). Propose merge.",
			"citations":     []map[string]any{{"anchor": "letter", "citation": matbCite(docID, ver)}},
		}}}},
		{Calls: []matbCall{matbSubmit(matter.GateContactMerge, mat05DraftID)}},
	}
	for _, c := range extra {
		turns = append(turns, matbTurn{Calls: []matbCall{c}})
	}
	return append(turns, matbTurn{Text: "Proposed one contact merge for approval."})
}

// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
// rule: AI-R-003
// rule: AI-R-005
func TestFMAT05_FromFeature(t *testing.T) {
	full := []string{"report_plan", "search_contacts", "get_matter", "search_documents", "read_document", "draft_document", "submit_for_approval"}
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
		{name: "kind outside the Def", extra: []matbCall{matbSubmit("trust.withdrawal", mat05DraftID)},
			wantTools:  append(slices.Clone(full), "submit_for_approval"),
			wantRefuse: map[string]error{"submit_for_approval": agenttools.ErrGateKindNotAllowed}},
		// A subject the run did not draft cannot be submitted.
		{name: "subject not drafted", extra: []matbCall{matbSubmit(matter.GateContactMerge, "draft-document-9")},
			wantTools:  append(slices.Clone(full), "submit_for_approval"),
			wantRefuse: map[string]error{"submit_for_approval": agenttools.ErrNotFound}},
		// rule: ARC-I-038
		{name: "injection", inject: true, wantTools: full},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			def := matter.ContactDedupeDef()
			fd := matbFeature(t, def)
			lines := []string{"ENGAGEMENT LETTER to Synth Logistics Inc., 1 Synthetic Way.", "Signed for Synth Logistics, 1 Synthetic Way."}
			if c.inject {
				lines = append(lines, matbInjected)
			}
			mid, docID, ver := matbSeed(fd, "2026-0501", "Synth logistics retainer", lines...)
			fd.Seed.Contact("Synth Logistics Inc.")
			fd.Seed.Contact("Synth Logistics")
			run := fd.Fire(context.Background(), matbScript(t, def.ID, mat05Script(mid, docID, ver, c.extra...)...),
				"run-fmat05-"+strings.ReplaceAll(c.name, " ", "-"), "find duplicate contacts")
			matbCheckRun(t, run, c.wantTools, agent.ToolDraftDocument, c.wantRefuse)
			// rule: ARC-I-039
			// The tool-gap kind opens only through the run's own draft, prepared by the acting user.
			if got := fd.Seed.Store.Drafts; len(got) != 1 || got[0].ID != mat05DraftID || got[0].PreparedBy != agenttest.FeatureActingFor || got[0].MatterID != mid {
				t.Fatalf("drafts = %+v", got)
			}
			gs := matbGateDecisions(t, fd, run, []string{matter.GateContactMerge})
			if g := gs[0]; g.Subject.Kind != matbSubjectDraft || g.Subject.ID != mat05DraftID {
				t.Fatalf("G-CM subject = %+v, want the run's draft", g.Subject)
			}
			if c.inject {
				if err := matbQuotedOnly(run, matbInjected); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(gs[0].Payload), "ignore previous") {
					t.Fatal("injected text in the G-CM payload")
				}
			}
		})
	}
}

// rule: AI-R-002
func TestFMAT05_Def(t *testing.T) {
	sf := matbSpecSync(t, matter.ContactDedupeDef())
	if sf.Outputs != "Merge and relationship proposals for G-CM" {
		t.Fatalf("spec output = %q", sf.Outputs)
	}
}

func TestFMAT05_Prompt(t *testing.T) {
	matbPrompt(t, matter.ContactDedupeDef(), "Contact dedupe and enrichment", "Merge and relationship proposals for G-CM.",
		"draft_document", "submit_for_approval", matter.GateContactMerge)
}

// F-MAT-05 runs daily for its designated lawyer and on each contact created, and on request
// from the contact list.
func TestFMAT05_TriggerBinding(t *testing.T) {
	b, err := matter.ContactDedupeBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	def := matter.ContactDedupeDef()
	tests := []struct {
		field     string
		got, want string
	}{
		{"feature", b.FeatureID, "F-MAT-05"},
		{"events", strings.Join(b.Events, ","), "contact.created"},
		{"cadence", string(b.Cadence), string(trigger.CadenceDaily)},
		{"designated lawyer", map[bool]string{true: "yes", false: "no"}[b.Designated], "yes"},
		{"acting for", b.ActingFor, ""},
		{"on request", strings.Join(def.Actions, ";"), "contact-list > Propose contact from email or document;contact-list > Propose merge"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}
