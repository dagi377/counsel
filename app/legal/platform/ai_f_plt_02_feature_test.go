package platform_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/comms"
	"github.com/dagi377/counsel/app/legal/platform"
	"github.com/dagi377/counsel/app/legal/timekeeping/timekeepingfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
)

// pltjMailDue is the reply date the synthetic email sets, on a line of its own.
const pltjMailDue = "2026-11-20"

// pltjMail is the synthetic inbound email filed on the matter, page 1.
var pltjMail = []string{
	"From: opposing counsel. Re: file 2026-0402, reply schedule.",
	"Your reply is due on:",
	pltjMailDue,
	"Our call of 2026-10-08 ran 18 minutes on the reply schedule.",
}

// F-PLT-02 starts on new mail through a connected mailbox, acting for the mailbox owner, and
// on request from the comms-log.
func TestFPLT02_TriggerBinding(t *testing.T) {
	b, err := platform.MailFilingBinding()
	pltcCheckBinding(t, b, err, platform.FeatureMailFiling, platform.EventEmailReceived)
	d := platform.MailFilingDef()
	tests := []struct {
		name      string
		got, want string
	}{
		{"event is the comms event", platform.EventEmailReceived, comms.EventEmailReceived},
		{"trigger", d.Trigger, "event: new mail via connector"},
		{"actions", strings.Join(d.Actions, ";"), platform.ActionAutoFileEmail + ";" + platform.ActionProposeTimeFromCm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// pltjMailCalls is the F-PLT-02 write script over w: plan, read, propose the reply deadline
// (G7) due on dueOn and the call's time (G9), then draft the filing review and submit it
// under filingKind (G-D1).
func pltjMailCalls(w pltcWorld, dueOn, filingKind string) []pltcCall {
	return []pltcCall{
		pltcPlan("read", "deadline", "time", "filing"),
		{agent.ToolGetMatter, map[string]any{"matter_id": w.matter}},
		{agent.ToolReadDocument, map[string]any{"doc_id": w.doc, "version_id": string(w.version)}},
		{agent.ToolProposeDeadline, map[string]any{"matter_id": w.matter, "title": "Serve the reply", "due_on": dueOn,
			"source": map[string]any{"doc_id": w.doc, "version_id": string(w.version), "page": 1, "line_start": 2, "line_end": 3,
				"quote_hash": "sha256:" + strings.Repeat("ab", 32)}}},
		{agent.ToolProposeTimeEntries, map[string]any{"entries": []map[string]any{{
			"matter_id": w.matter, "work_date": "2026-10-08", "minutes": 18, "narrative": "Call with opposing counsel re reply schedule.",
		}}}},
		{agent.ToolDraftDocument, map[string]any{"matter_id": w.matter, "title": "Filing review: inbound email",
			"body_markdown": "Possibly without prejudice; review before filing (page 1, lines 1 to 4)."}},
		{agent.ToolSubmitForApproval, map[string]any{"subject_type": "document", "subject_id": "00000000-0000-4000-8000-00000000f001", "gate_kind": filingKind}},
	}
}

// pltjMailRun fires F-PLT-02 once over the real handlers, the email holding lines.
func pltjMailRun(t *testing.T, lines []string, dueOn, filingKind string) (pltcWorld, []pltcCall, agenttest.FeatureRun) {
	t.Helper()
	w := pltcFeature(t, platform.MailFilingDef(), lines, deadlinefake.New(), timekeepingfake.New(nil, nil))
	calls := pltjMailCalls(w, dueOn, filingKind)
	model := pltcScript(t, platform.FeatureMailFiling, calls, "Proposed the reply deadline for G7, the call time for G9 and the filing review for G-D1.")
	return w, calls, w.fd.Fire(context.Background(), model, "run-fplt02-write", "email received")
}

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-039
func TestFPLT02_FromFeature(t *testing.T) {
	w, calls, run := pltjMailRun(t, pltjMail, pltjMailDue, "docs.filing_privilege")
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	// Every tool of the script runs its real handler.
	if len(w.fd.StandIns) != 0 || len(run.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want none", w.fd.StandIns, run.StandIns)
	}
	if want := pltcToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v", run.Tools, want)
	}
	// rule: ARC-I-039
	if run.PlanAt != 0 || run.FirstWrite <= run.PlanAt || run.Tools[run.FirstWrite] != agent.ToolProposeDeadline {
		t.Fatalf("plan at %d, first write %d in %v; want report_plan before propose_deadline", run.PlanAt, run.FirstWrite, run.Tools)
	}
	kinds := []string{agent.GateCalConfirm, agent.GateTimeAccept, "docs.filing_privilege"}
	if len(run.GateIDs) != len(kinds) {
		t.Fatalf("gates = %v, want G7, G9 and G-D1", run.GateIDs)
	}
	// Each gate is pending for the acting user as preparer: the agent principal deciding is
	// 403, the acting user deciding is ErrSelfApproval (403), another lawyer may decide.
	for i, kind := range kinds {
		t.Run(kind, func(t *testing.T) { pltcCheckGate(t, w.fd, run.GateIDs[i], kind) })
	}
}

// rule: ARC-I-035
// rule: ARC-U-027
// docs.filing_privilege is a tool-gap kind: submit_for_approval opens it on the run's draft
// and refuses any kind outside the Def through the scoped opener. A due date the run never
// read is refused before any deadline is stored.
func TestFPLT02_WritePathRefused(t *testing.T) {
	tests := []struct {
		name       string
		dueOn      string
		filingKind string
		tool       string
		wantErr    error
		gates      int
	}{
		{"outgoing email kind", pltjMailDue, "platform.outgoing_email", agent.ToolSubmitForApproval, agenttools.ErrGateKindNotAllowed, 2},
		{"another documents kind", pltjMailDue, "docs.template_publish", agent.ToolSubmitForApproval, agenttools.ErrGateKindNotAllowed, 2},
		{"agent note kind", pltjMailDue, "matter.agent_note", agent.ToolSubmitForApproval, agenttools.ErrGateKindNotAllowed, 2},
		{"made-up due date", "2026-12-01", "docs.filing_privilege", agent.ToolProposeDeadline, agenttools.ErrUnsourcedValue, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, run := pltjMailRun(t, pltjMail, tt.dueOn, tt.filingKind)
			if len(run.Refused) != 1 || run.Refused[0].Tool != tt.tool || !errors.Is(run.Refused[0].Err, tt.wantErr) {
				t.Fatalf("refused = %v, want %s refused with %v", run.Refused, tt.tool, tt.wantErr)
			}
			if len(run.GateIDs) != tt.gates {
				t.Fatalf("gates = %v, want %d", run.GateIDs, tt.gates)
			}
		})
	}
}

// rule: ARC-I-038
func TestFPLT02_WritePathInjection(t *testing.T) {
	w, calls, run := pltjMailRun(t, append(slices.Clone(pltjMail), pltcInjected+"."), pltjMailDue, "docs.filing_privilege")
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	if want := pltcToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v (no extra call)", run.Tools, want)
	}
	if err := agenttest.QuotedOnly(run.Run, pltcInjected); err != nil {
		t.Fatal(err)
	}
	// Neither write input nor any gate payload carries the injected text.
	if err := pltcAbsent(context.Background(), w.fd, run, pltcInjected); err != nil {
		t.Fatal(err)
	}
}
