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

// rdy08Email is the synthetic selected email, filed on its matter.
var rdy08Email = []string{
	"Re: Synth Readiness, court file CV-26-0412. Can you confirm the filing deadline?",
	"Filing deadline is 2026-11-16 under the timetable order.",
}

// rdy08Reply drafts the reply to the sender on w's matter, with submit as given.
func rdy08Reply(w rdycWorld, submit bool) rdycCall {
	s := "false"
	if submit {
		s = "true"
	}
	return rdycCall{agent.ToolDraftEmail, `{"matter_id": "` + w.matterID + `", "to_contact_ids": ["` + w.clientID + `"], "subject": "Re: filing deadline", "body_markdown": "The filing deadline is 2026-11-16 under the timetable order.", "submit": ` + s + `}`}
}

// rdy08Calls is the F-RDY-08 script: find the email, plan, draft the reply and submit it to
// the outgoing-email gate in the same call.
func rdy08Calls(w rdycWorld) []rdycCall {
	return []rdycCall{rdycSearch(w, "deadline"), rdycPlan, rdy08Reply(w, true)}
}

func rdy08Run(t *testing.T, calls func(rdycWorld) []rdycCall, lines ...string) (rdycWorld, agenttest.FeatureRun, []rdycCall) {
	t.Helper()
	w := rdycFeature(agenttest.FromFeature(t, readiness.DraftReplyDef()), lines...)
	c := calls(w)
	model := rdycScript("F-RDY-08", c, "Drafted the reply for release.")
	prompt := "screen: mail-add-ins\nrecords: matter:" + w.matterID + "\nsender contact: " + w.clientID
	return w, w.fd.Fire(context.Background(), model, "run-frdy08", prompt), c
}

// rule: AI-R-003
// rule: ARC-I-035
func TestFRDY08_DefMatchesSpec(t *testing.T) {
	rdycCheckDefMatchesSpec(t, readiness.DraftReplyDef())
}

// rule: AI-R-005
func TestFRDY08_Prompt(t *testing.T) {
	rdycCheckPrompt(t, readiness.DraftReplyDef(),
		"Your task: Draft reply",
		"draft reply on mail-add-ins (Goes to the documents release gate before sending)",
		agent.GateOutgoingEmail, "report_plan", "draft_email", "submit true")
}

// F-RDY-08 runs on request from the mail add-in's Draft reply action.
func TestFRDY08_TriggerBinding(t *testing.T) {
	rdycCheckAction(t, readiness.DraftReplyDef(), readiness.ActionDraftReply)
}

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-039
// Every tool runs its real handler: the plan comes before draft_email, the draft is stored
// for the acting user and waits at platform.outgoing_email, which only another person decides.
func TestFRDY08_FromFeature(t *testing.T) {
	w, run, calls := rdy08Run(t, rdy08Calls, rdy08Email...)
	rdycCheckRun(t, w, run, calls, agent.ToolDraftEmail, agent.GateOutgoingEmail)
	rdycCheckEmailDraft(t, w)
}

// rule: ARC-I-035
// The reply may wait only at the outgoing-email gate: submitting the draft to the portal
// message gate, or any other kind, is refused and no gate opens.
func TestFRDY08_GateKindNotAllowed(t *testing.T) {
	for _, kind := range []string{docs.GatePortalMessage, "trust.withdrawal"} {
		t.Run(kind, func(t *testing.T) {
			w, run, _ := rdy08Run(t, func(w rdycWorld) []rdycCall {
				return []rdycCall{rdycSearch(w, "deadline"), rdycPlan, rdy08Reply(w, false),
					{agent.ToolSubmitForApproval, `{"subject_type": "` + agent.SubjectEmailDraft + `", "subject_id": "` + w.draftID("email") + `", "gate_kind": "` + kind + `"}`}}
			}, rdy08Email...)
			rdycCheckRefused(t, run, agent.ToolSubmitForApproval, agenttools.ErrGateKindNotAllowed)
			rdycCheckEmailDraft(t, w)
		})
	}
}

// rule: ARC-I-038
func TestFRDY08_Injection(t *testing.T) {
	lines := append(slices.Clone(rdy08Email), "About the deadline: "+rdycInjected+".")
	w, run, calls := rdy08Run(t, rdy08Calls, lines...)
	rdycCheckInjection(t, w, run, calls, rdycInjected)
}
