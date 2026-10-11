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
// F-INT-17 runs on request only, from intake-automations > Draft sequence from description.
func TestFINT17_DefMatchesSpec(t *testing.T) {
	inteCheckDefMatchesSpec(t, intake.DraftSequenceDef(), intake.ActionDraftSequence)
}

// rule: AI-R-005
func TestFINT17_Prompt(t *testing.T) {
	inteCheckPrompt(t, intake.DraftSequenceDef(),
		"Draft sequence from description", "Draft sequence from description on intake-automations",
		inteSkillAutonomy, "report_plan", "draft_document", "submit_for_approval")
}

// fint17Run seeds a matter named after short, then runs F-INT-17 with a script that submits
// the draft sequence under gateKind.
func fint17Run(t *testing.T, short, gateKind string) (*agenttest.FeatureDriver, *inteDrafts, agenttest.FeatureRun, []string) {
	t.Helper()
	fd, drafts := inteFeature(t, intake.DraftSequenceDef())
	m := fd.Seed.Matter("2026-0117", short)
	calls := inteDocCalls("Synth Sequence", string(m.ID), "Draft sequence: consult follow-up",
		"1. Day 0, email: thank you for your enquiry. 2. Day 2, email: book a consultation. 3. Day 5, call: check in.", gateKind)
	model := inteScript("F-INT-17", calls, "Drafted the sequence for review.")
	return fd, drafts, fd.Fire(context.Background(), model, "run-fint17", intake.ActionDraftSequence), inteToolNames(calls)
}

// rule: ARC-I-035
// rule: ARC-I-039
func TestFINT17_FromFeature(t *testing.T) {
	fd, drafts, run, _ := fint17Run(t, "Synth Sequence", inteSkillAutonomy)
	inteCheckRun(t, fd, run, agent.ToolDraftDocument, inteSkillAutonomy)
	inteCheckDocDraft(t, drafts)
}

// rule: ARC-I-035
// platform.skill_autonomy is a tool-gap kind: draft_document then submit_for_approval opens
// it, and submit_for_approval may open no kind outside the Def.
func TestFINT17_GateKindNotAllowed(t *testing.T) {
	for _, kind := range []string{inteConsultOutcome, "intake.lead_decision", "matter.opening", "trust.withdrawal"} {
		t.Run(kind, func(t *testing.T) {
			_, drafts, run, _ := fint17Run(t, "Synth Sequence", kind)
			inteCheckKindRefused(t, drafts, run)
		})
	}
}

// rule: ARC-I-038
func TestFINT17_Injection(t *testing.T) {
	fd, _, run, want := fint17Run(t, "Synth Sequence "+inteInjected, inteSkillAutonomy)
	inteCheckInjection(t, fd, run, want, inteInjected)
}
