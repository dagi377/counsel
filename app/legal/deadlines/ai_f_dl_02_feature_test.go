package deadlines

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/gate"
)

// limitationScript is one F-DL-02 run: plan, read the intake, compute the limitation date,
// propose it, then the extra calls. The intake states discovery on 2026-10-05.
func limitationScript(mid, docID, ver, dueOn string, extra ...call) []turn {
	turns := []turn{
		{Calls: []call{plan("read", "compute", "propose", "second-check")}},
		{Calls: []call{{Name: "get_matter", Input: map[string]any{"matter_id": mid}}}},
		{Calls: []call{{Name: "read_document", Input: map[string]any{"doc_id": docID, "version_id": ver}}}},
		{Calls: []call{{Name: "list_deadlines", Input: map[string]any{"matter_id": mid}}}},
		{Calls: []call{{Name: "compute_deadline", Input: map[string]any{"rule_id": "syn-civil/limitation", "trigger_date": "2026-10-05"}}}},
		{Calls: []call{{Name: "propose_deadline", Input: map[string]any{
			"matter_id": mid, "title": "Last day to start the claim", "due_on": dueOn,
			"rule_id": "syn-civil/limitation", "trigger_date": "2026-10-05", "source": source(docID, ver),
		}}}},
	}
	for _, c := range extra {
		turns = append(turns, turn{Calls: []call{c}})
	}
	return append(turns, turn{Text: "Drafted the limitation assessment for sign-off."})
}

func submit(kind, subjectID string) call {
	return call{Name: agent.ToolSubmitForApproval, Input: map[string]any{"subject_type": SubjectDraft, "subject_id": subjectID, "gate_kind": kind}}
}

// rule: ARC-I-035
// rule: ARC-I-039
// rule: AI-R-003
func TestFDL02_FromFeature(t *testing.T) {
	full := []string{"report_plan", "get_matter", "read_document", "list_deadlines", "compute_deadline", "propose_deadline"}
	cases := []struct {
		name       string
		dueOn      string
		inject     bool
		extra      []call
		wantTools  []string
		wantGates  []string
		wantRefuse map[string]error // tool -> error
	}{
		{name: "ok", dueOn: "2028-10-05", wantTools: full, wantGates: []string{agent.GateCalConfirm}},
		// rule: ARC-I-035
		{name: "model-computed date", dueOn: "2028-10-06", wantTools: full,
			wantRefuse: map[string]error{"propose_deadline": agenttools.ErrUnsourcedValue}},
		// rule: AI-R-003
		// A gate kind outside the Def is refused before anything opens.
		{name: "kind outside the Def", dueOn: "2028-10-05", extra: []call{submit("trust.withdrawal", "draft-1")},
			wantTools: append(slices.Clone(full), "submit_for_approval"), wantGates: []string{agent.GateCalConfirm},
			wantRefuse: map[string]error{"submit_for_approval": agenttools.ErrGateKindNotAllowed}},
		// The Def's own tools draft nothing, so G7b cannot be opened on a subject the run did
		// not draft (docs/QUESTIONS.md Q-FEAT-DL-04).
		{name: "second check without a draft", dueOn: "2028-10-05", extra: []call{submit(GateCalSecondCheck, "draft-1")},
			wantTools: append(slices.Clone(full), "submit_for_approval"), wantGates: []string{agent.GateCalConfirm},
			wantRefuse: map[string]error{"submit_for_approval": agenttools.ErrNotFound}},
		// rule: ARC-I-038
		{name: "injection", dueOn: "2028-10-05", inject: true, wantTools: full, wantGates: []string{agent.GateCalConfirm}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			def := LimitationAssessmentDef()
			fd, w := dlFeature(t, def)
			if len(fd.StandIns) != 0 {
				t.Fatalf("stand-ins = %v, want none", fd.StandIns)
			}
			lines := []string{"Intake: the client discovered the water damage on 2026-10-05.", "No claim has been started."}
			if c.inject {
				lines = append(lines, injected)
			}
			mid, docID, ver := dlSeed(t, fd, w, lines...)
			fd.Seed.Deadline(mid, "syn-civil/reply", deadline.Date{Year: 2026, Month: 12, Day: 1})
			run := fd.Fire(context.Background(), script(t, def.ID, limitationScript(mid, docID, ver, c.dueOn, c.extra...)...),
				"run-fdl02-"+strings.ReplaceAll(c.name, " ", "-"), "draft the limitation assessment from the intake")
			if run.Err != nil {
				t.Fatalf("run: %v", run.Err)
			}
			if !slices.Equal(run.Tools, c.wantTools) {
				t.Fatalf("tools = %v, want %v (no extra call)", run.Tools, c.wantTools)
			}
			// rule: AI-R-003
			if run.PlanAt != 0 {
				t.Fatalf("plan at %d, want 0", run.PlanAt)
			}
			if c.wantRefuse["propose_deadline"] == nil && run.FirstWrite <= run.PlanAt {
				t.Fatalf("first write %d does not follow the plan", run.FirstWrite)
			}
			if len(run.Refused) != len(c.wantRefuse) {
				t.Fatalf("refused = %v, want %v", run.Refused, c.wantRefuse)
			}
			for _, r := range run.Refused {
				if want := c.wantRefuse[r.Tool]; want == nil || !errors.Is(r.Err, want) {
					t.Fatalf("refused %s: %v, want %v", r.Tool, r.Err, want)
				}
			}
			if c.wantRefuse["propose_deadline"] != nil {
				if len(run.GateIDs) != 0 {
					t.Fatalf("unsourced date opened gates %v", run.GateIDs)
				}
				if got, _ := w.Deadlines.List(context.Background(), deadline.Filter{}); len(got) != 0 {
					t.Fatalf("unsourced date saved %d deadlines", len(got))
				}
				return
			}
			gateDecisions(t, fd, run, c.wantGates)
			if c.inject {
				if err := docQuotedOnly(run, injected); err != nil {
					t.Fatal(err)
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
		})
	}
}

// rule: AI-R-003
// rule: ARC-I-039
// The tool-gap kind cal.second_check (G7b) opens only by drafting the warning and submitting
// that draft. The Def's tool list has no drafting tool (Q-FEAT-DL-04), so this run adds
// draft_document and its capability to the Def; every other check is the production path.
func TestFDL02_SecondCheckViaDraft(t *testing.T) {
	def := LimitationAssessmentDef()
	def.Tools = append(slices.Clone(def.Tools), agent.ToolDraftDocument)
	def.Grant = append(slices.Clone(def.Grant), "documents.edit")
	fd, w := dlFeature(t, def)
	if len(fd.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, want none", fd.StandIns)
	}
	mid, docID, ver := dlSeed(t, fd, w, "Intake: the client discovered the water damage on 2026-10-05.", "The other driver is not our client.")
	draft := call{Name: agent.ToolDraftDocument, Input: map[string]any{
		"matter_id": mid, "title": "Limitation warning to non-client",
		"body_markdown": "A limitation period may apply to your claim. Get legal advice.",
	}}
	// The SeedStore numbers records in one sequence: the intake document is 1, the warning
	// draft 2. submit_for_approval refuses any subject the run did not draft.
	const draftID = "draft-document-2"
	run := fd.Fire(context.Background(), script(t, def.ID, limitationScript(mid, docID, ver, "2028-10-05", draft, submit(GateCalSecondCheck, draftID))...),
		"run-fdl02-g7b", "draft the limitation assessment and the non-client warning")
	if run.Err != nil {
		t.Fatalf("run: %v", run.Err)
	}
	if len(run.Refused) != 0 {
		t.Fatalf("refused = %v", run.Refused)
	}
	if got := fd.Seed.Store.Drafts; len(got) != 1 || got[0].ID != draftID || got[0].PreparedBy != "user-synthetic-lawyer" {
		t.Fatalf("drafts = %+v", got)
	}
	gateDecisions(t, fd, run, []string{agent.GateCalConfirm, GateCalSecondCheck})
	for _, id := range run.GateIDs {
		g, _ := fd.Seed.Gates.Get(context.Background(), gate.GateID(id))
		if g.Kind == GateCalSecondCheck && (g.Subject.Kind != SubjectDraft || g.Subject.ID != draftID) {
			t.Fatalf("G7b subject = %+v", g.Subject)
		}
	}
}

// F-DL-02 starts from an intake or a claim-facts change, and on request from its screen.
func TestFDL02_TriggerBinding(t *testing.T) {
	b, err := LimitationAssessmentBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	def := LimitationAssessmentDef()
	tests := []struct {
		field     string
		got, want string
	}{
		{"feature", b.FeatureID, "F-DL-02"},
		{"events", strings.Join(b.Events, ","), "lead.created,matter.updated"},
		{"cadence", string(b.Cadence), string(trigger.CadenceNone)},
		{"acting for", b.ActingFor, ""},
		{"trigger", def.Trigger, "event: intake created or claim facts updated"},
		{"on request", strings.Join(def.Actions, ";"), "limitation-assessment > Draft assessment from intake;limitation-assessment > Send limitation warning to non-client"},
		{"gate kinds", strings.Join(def.GateKinds, ","), agent.GateCalConfirm + "," + GateCalSecondCheck},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}

func TestFDL02_SecondCheckInput(t *testing.T) {
	tests := []struct {
		name    string
		draftID string
		wantErr error
	}{
		{"draft", "draft-1", nil},
		{"no draft", "", ErrBadProposal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := SecondCheckInput(tt.draftID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			var in map[string]string
			if err := json.Unmarshal(b, &in); err != nil || in["gate_kind"] != GateCalSecondCheck || in["subject_id"] != tt.draftID || in["subject_type"] != SubjectDraft {
				t.Fatalf("input = %s (%v)", b, err)
			}
		})
	}
}
