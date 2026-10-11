package matter_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFMAT19_DefMatchesSpec(t *testing.T) {
	matfCheckDefMatchesSpec(t, matter.RequestIDDef())
}

// rule: AI-R-005
func TestFMAT19_Prompt(t *testing.T) {
	d := matter.RequestIDDef()
	matfCheckPrompt(t, d)
	spec, err := d.Spec()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{matter.GateClientID, "report_plan", "draft_document", "submit_for_approval"} {
		if !strings.Contains(spec.System, w) {
			t.Errorf("system prompt lacks %q", w)
		}
	}
}

// F-MAT-19 runs only from its screen action, contact-detail > Request ID.
func TestFMAT19_TriggerBinding(t *testing.T) {
	d := matter.RequestIDDef()
	tests := []struct {
		field     string
		got, want string
	}{
		{"source", string(d.Source), string(feature.SourceScreenAction)},
		{"trigger", d.Trigger, "on request: contact-detail > Request ID"},
		{"actions", strings.Join(d.Actions, ";"), "contact-detail > Request ID"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}

// mat19World is one F-MAT-19 run's seeded matter, the client's ID record on it and the id
// the Seed gives the run's draft.
type mat19World struct {
	matterID, docID, versionID, draftID string
}

// mat19Calls is the F-MAT-19 script: find the client and the matter, read the ID record,
// plan, draft the portal request, submit it under gateKind.
func mat19Calls(w mat19World, gateKind string) []matfCall {
	return []matfCall{
		{agent.ToolSearchContacts, `{"query": "Request client", "limit": 5}`},
		{agent.ToolGetMatter, `{"matter_id": "` + w.matterID + `"}`},
		{agent.ToolSearchDocuments, `{"matter_id": "` + w.matterID + `", "query": "identification"}`},
		{agent.ToolReadDocument, `{"doc_id": "` + w.docID + `", "version_id": "` + w.versionID + `"}`},
		{agenttools.ToolReportPlan, `{"steps": [{"step_id": "check", "title": "Check the ID on file"}, {"step_id": "request", "title": "Draft the ID request"}]}`},
		{agent.ToolDraftDocument, `{"matter_id": "` + w.matterID + `", "title": "Request for identification", "body_markdown": "Please upload one government photo ID through the portal so we can verify your identity."}`},
		{agent.ToolSubmitForApproval, `{"subject_type": "document", "subject_id": "` + w.draftID + `", "gate_kind": "` + gateKind + `"}`},
	}
}

// mat19Run seeds one matter with an ID record whose lines are idLines and fires F-MAT-19
// with the script's last call under gateKind.
func mat19Run(t *testing.T, gateKind string, idLines ...string) (*agenttest.FeatureDriver, agenttest.FeatureRun, []matfCall) {
	t.Helper()
	fd := matfFeature(t, matter.RequestIDDef())
	m := fd.Seed.Matter("2026-0419", "Request")
	docID, v := fd.Seed.Document(string(m.ID), idLines)
	// The Seed numbers documents and drafts in one sequence: the draft follows the document.
	calls := mat19Calls(mat19World{matterID: string(m.ID), docID: docID, versionID: string(v), draftID: "draft-document-2"}, gateKind)
	model := matfScript(matter.FeatureRequestID, calls, "Drafted the ID request for approval.")
	return fd, fd.Fire(context.Background(), model, "run-fmat19", "contact-detail > Request ID"), calls
}

// rule: ARC-I-035
// rule: ARC-I-039
// rule: AI-R-003
func TestFMAT19_FromFeature(t *testing.T) {
	fd, run, calls := mat19Run(t, matter.GateClientID, "ID record: none on file.")
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	// Every tool of the Def runs its real handler.
	if len(fd.StandIns) != 0 || len(run.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want none", fd.StandIns, run.StandIns)
	}
	if want := matfToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v", run.Tools, want)
	}
	// rule: ARC-I-039
	if run.PlanAt < 0 || run.FirstWrite <= run.PlanAt || run.Tools[run.FirstWrite] != agent.ToolDraftDocument {
		t.Fatalf("plan at %d, first write %d in %v; want report_plan before draft_document", run.PlanAt, run.FirstWrite, run.Tools)
	}
	// The request is drafted for the acting user, never in the agent's name.
	if d := fd.Seed.Store.Drafts; len(d) != 1 || d[0].PreparedBy != agenttest.FeatureActingFor {
		t.Fatalf("drafts = %+v, want one prepared by %s", d, agenttest.FeatureActingFor)
	}
	matfCheckGate(t, fd, run, matter.GateClientID)
}

// rule: ARC-I-035
// submit_for_approval may open only the Def's gate kind: any other kind is refused and no
// gate opens.
func TestFMAT19_GateKindNotAllowed(t *testing.T) {
	for _, kind := range []string{"conflict.clearance", "matter.opening", "trust.withdrawal"} {
		t.Run(kind, func(t *testing.T) {
			fd, run, _ := mat19Run(t, kind, "ID record: none on file.")
			if len(run.Refused) != 1 || run.Refused[0].Tool != agent.ToolSubmitForApproval || !errors.Is(run.Refused[0].Err, agenttools.ErrGateKindNotAllowed) {
				t.Fatalf("refused = %v, want submit_for_approval refused with ErrGateKindNotAllowed", run.Refused)
			}
			if len(run.GateIDs) != 0 || len(fd.Seed.Store.Drafts) != 1 {
				t.Fatalf("gates opened: %v, drafts %d; want none and the one draft", run.GateIDs, len(fd.Seed.Store.Drafts))
			}
		})
	}
}

// rule: ARC-I-038
func TestFMAT19_Injection(t *testing.T) {
	fd, run, calls := mat19Run(t, matter.GateClientID, "ID record: passport seen 2019.", matfInjected)
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	if want := matfToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v (no extra call)", run.Tools, want)
	}
	if err := agenttest.QuotedOnly(run.Run, matfInjected); err != nil {
		t.Fatal(err)
	}
	if err := matfAbsent(context.Background(), fd, run, matfInjected); err != nil {
		t.Fatal(err)
	}
}
