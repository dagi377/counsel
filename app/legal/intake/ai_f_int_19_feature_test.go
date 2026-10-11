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
// F-INT-19 runs on request only, from ai-receptionist > Suggest criteria from firm website.
func TestFINT19_DefMatchesSpec(t *testing.T) {
	inteCheckDefMatchesSpec(t, intake.ReceptionistCriteriaDef(), intake.ActionSuggestCriteria)
}

// rule: AI-R-005
func TestFINT19_Prompt(t *testing.T) {
	inteCheckPrompt(t, intake.ReceptionistCriteriaDef(),
		"Suggest criteria from firm website", "Suggest criteria from firm website on ai-receptionist",
		"Agent drafts profiles and FAQ from the firm site; a lawyer approves (G-form)",
		inteSkillAutonomy, "report_plan", "draft_document", "submit_for_approval")
}

// fint19Run seeds a matter named after short, then runs F-INT-19 with a script that submits
// the draft criteria under gateKind.
func fint19Run(t *testing.T, short, gateKind string) (*agenttest.FeatureDriver, *inteDrafts, agenttest.FeatureRun, []string) {
	t.Helper()
	fd, drafts := inteFeature(t, intake.ReceptionistCriteriaDef())
	m := fd.Seed.Matter("2026-0119", short)
	calls := inteDocCalls("Synth Firm", string(m.ID), "Draft receptionist criteria",
		"Practice profile: commercial litigation in Ontario. Take: contract disputes. Refer: family law. FAQ: consultations are 30 minutes.", gateKind)
	model := inteScript("F-INT-19", calls, "Drafted the criteria for review.")
	return fd, drafts, fd.Fire(context.Background(), model, "run-fint19", intake.ActionSuggestCriteria), inteToolNames(calls)
}

// rule: ARC-I-035
// rule: ARC-I-039
func TestFINT19_FromFeature(t *testing.T) {
	fd, drafts, run, _ := fint19Run(t, "Synth Firm", inteSkillAutonomy)
	inteCheckRun(t, fd, run, agent.ToolDraftDocument, inteSkillAutonomy)
	inteCheckDocDraft(t, drafts)
}

// rule: ARC-I-035
// platform.skill_autonomy is a tool-gap kind: draft_document then submit_for_approval opens
// it, and submit_for_approval may open no kind outside the Def.
func TestFINT19_GateKindNotAllowed(t *testing.T) {
	for _, kind := range []string{inteConsultOutcome, "intake.lead_decision", "matter.opening", "trust.withdrawal"} {
		t.Run(kind, func(t *testing.T) {
			_, drafts, run, _ := fint19Run(t, "Synth Firm", kind)
			inteCheckKindRefused(t, drafts, run)
		})
	}
}

// rule: ARC-I-038
func TestFINT19_Injection(t *testing.T) {
	fd, _, run, want := fint19Run(t, "Synth Firm "+inteInjected, inteSkillAutonomy)
	inteCheckInjection(t, fd, run, want, inteInjected)
}
