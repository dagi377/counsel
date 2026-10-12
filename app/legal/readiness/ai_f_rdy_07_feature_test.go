package readiness_test

import (
	"context"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/readiness"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// rdy07Message is the synthetic client portal message the reply answers.
var rdy07Message = []string{
	"Portal message from the client: when is the motion hearing?",
	"Hearing set for 2026-11-02 before the motions judge.",
}

// rdy07Calls is the F-RDY-07 script: find the message, plan, draft the reply without sending
// it, submit it under gateKind.
func rdy07Calls(w rdycWorld, gateKind string) []rdycCall {
	return []rdycCall{
		rdycSearch(w, "hearing"),
		rdycPlan,
		{agent.ToolDraftEmail, `{"matter_id": "` + w.matterID + `", "to_contact_ids": ["` + w.clientID + `"], "subject": "Your motion hearing", "body_markdown": "The hearing is set for 2026-11-02 before the motions judge.", "submit": false}`},
		{agent.ToolSubmitForApproval, `{"subject_type": "` + agent.SubjectEmailDraft + `", "subject_id": "` + w.draftID("email") + `", "gate_kind": "` + gateKind + `"}`},
	}
}

func rdy07Run(t *testing.T, gateKind string, lines ...string) (rdycWorld, agenttest.FeatureRun, []rdycCall) {
	t.Helper()
	w := rdycFeature(agenttest.FromFeature(t, readiness.ReplyToClientMessageDef()), lines...)
	calls := rdy07Calls(w, gateKind)
	model := rdycScript("F-RDY-07", calls, "Drafted a reply for approval.")
	prompt := "screen: mobile-app\nrecords: matter:" + w.matterID + "\nclient contact: " + w.clientID
	return w, w.fd.Fire(context.Background(), model, "run-frdy07", prompt), calls
}

// rule: AI-R-003
// rule: ARC-I-035
func TestFRDY07_DefMatchesSpec(t *testing.T) {
	rdycCheckDefMatchesSpec(t, readiness.ReplyToClientMessageDef())
}

// rule: AI-R-005
func TestFRDY07_Prompt(t *testing.T) {
	rdycCheckPrompt(t, readiness.ReplyToClientMessageDef(),
		"Your task: Reply to client text or portal message",
		"reply to client text or portal message on mobile-app (Drafts only; sent after the same send gate as the web)",
		docs.GatePortalMessage, "report_plan", "draft_email", "submit false")
}

// F-RDY-07 runs on request from the mobile app's Reply to client text or portal message action.
func TestFRDY07_TriggerBinding(t *testing.T) {
	rdycCheckAction(t, readiness.ReplyToClientMessageDef(), readiness.ActionReplyToClientMessage)
}

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-039
// Every tool runs its real handler: the plan comes before draft_email, the draft is stored
// for the acting user and waits at platform.portal_message, which only another person decides.
func TestFRDY07_FromFeature(t *testing.T) {
	w, run, calls := rdy07Run(t, docs.GatePortalMessage, rdy07Message...)
	rdycCheckRun(t, w, run, calls, agent.ToolDraftEmail, docs.GatePortalMessage)
	rdycCheckEmailDraft(t, w)
}

// rule: ARC-I-035
// submit_for_approval may open only the Def's gate kind: the outgoing-email gate and any
// other kind are refused, no gate opens and the reply stays a draft.
func TestFRDY07_GateKindNotAllowed(t *testing.T) {
	for _, kind := range []string{agent.GateOutgoingEmail, "trust.withdrawal"} {
		t.Run(kind, func(t *testing.T) {
			w, run, _ := rdy07Run(t, kind, rdy07Message...)
			rdycCheckRefused(t, run, agent.ToolSubmitForApproval, agenttools.ErrGateKindNotAllowed)
			rdycCheckEmailDraft(t, w)
		})
	}
}

// rule: ARC-I-038
func TestFRDY07_Injection(t *testing.T) {
	lines := append(slices.Clone(rdy07Message), "Portal message about the hearing: "+rdycInjected+".")
	w, run, calls := rdy07Run(t, docs.GatePortalMessage, lines...)
	rdycCheckInjection(t, w, run, calls, rdycInjected)
}
