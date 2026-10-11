package intake_test

import (
	"context"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/intake"
)

// rule: AI-R-003
// rule: ARC-I-035
// F-INT-15 runs on request only, from consult-outcome > Generate non-engagement letter.
func TestFINT15_DefMatchesSpec(t *testing.T) {
	inteCheckDefMatchesSpec(t, intake.NonEngagementLetterDef(), intake.ActionNonEngagementLetter)
}

// rule: AI-R-005
func TestFINT15_Prompt(t *testing.T) {
	inteCheckPrompt(t, intake.NonEngagementLetterDef(),
		"Generate non-engagement letter",
		"Generate non-engagement letter on consult-outcome (States what was and was not done and mentions limitation risk)",
		intake.GateConsultOutcome, "report_plan", "draft_document", "submit_for_approval", "limitation risk")
}

// fint15Letter is the synthetic non-engagement letter the scripted model drafts.
const fint15Letter = "Thank you for meeting with us. We held one consultation and will not act for you: we were not retained, gave no advice to rely on and took no steps. A limitation period may apply to your claim; get other legal advice promptly."

// fint15Run seeds the consultation matter named after short, then runs F-INT-15 with a script
// that drafts the letter with draft_document and submits it under gateKind (Q-FEAT-INT-11).
func fint15Run(t *testing.T, short, gateKind string) (*agenttest.FeatureDriver, *inteDrafts, agenttest.FeatureRun, []string) {
	t.Helper()
	fd, drafts := inteFeature(t, intake.NonEngagementLetterDef())
	m := fd.Seed.Matter("2026-0115", short)
	calls := inteDocCalls("Synth Consult", string(m.ID), "Non-engagement letter", fint15Letter, gateKind)
	model := inteScript("F-INT-15", calls, "Drafted the non-engagement letter for review.")
	return fd, drafts, fd.Fire(context.Background(), model, "run-fint15", intake.ActionNonEngagementLetter), inteToolNames(calls)
}

// rule: ARC-I-035
// rule: ARC-I-039
// The letter is drafted for the acting user and G3 opens pending on it: neither the agent nor
// the acting user can decide it, another lawyer can.
func TestFINT15_FromFeature(t *testing.T) {
	fd, drafts, run, _ := fint15Run(t, "Synth Consult", intake.GateConsultOutcome)
	inteCheckRun(t, fd, run, agent.ToolDraftDocument, intake.GateConsultOutcome)
	inteCheckDocDraft(t, drafts)
}

// rule: ARC-I-035
// intake.consult_outcome is F-INT-15's one gate kind; submitting the letter under any other
// kind is refused and no gate opens.
func TestFINT15_GateKindNotAllowed(t *testing.T) {
	for _, kind := range []string{"intake.lead_decision", "intake.open_matter", "matter.agent_note"} {
		t.Run(kind, func(t *testing.T) {
			_, drafts, run, _ := fint15Run(t, "Synth Consult", kind)
			inteCheckKindRefused(t, drafts, run)
		})
	}
}

// rule: ARC-I-038
func TestFINT15_Injection(t *testing.T) {
	fd, _, run, want := fint15Run(t, "Synth Consult "+inteInjected, intake.GateConsultOutcome)
	inteCheckInjection(t, fd, run, want, inteInjected)
}
