package intake_test

import (
	"context"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/intake"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFINT03_DefMatchesSpec(t *testing.T) {
	intbCheckDefMatchesSpec(t, intake.MissingInfoChaserDef())
}

// rule: AI-R-005
func TestFINT03_Prompt(t *testing.T) {
	intbCheckPrompt(t, intake.MissingInfoChaserDef(),
		"Missing-info chaser", "Updated lead fields", intbSkillAutonomy, "report_plan", "draft_document", "submit_for_approval")
}

// F-INT-03 starts when a lead is set to await information, acting for the event's user;
// daily for its designated lawyer, read from firm settings at each tick; and on request from
// lead-detail.
func TestFINT03_TriggerBinding(t *testing.T) {
	b, err := intake.MissingInfoChaserBinding()
	intbCheckBinding(t, b, err, "F-INT-03", []string{intake.EventLeadAwaitingInfo}, trigger.CadenceDaily, true)
	if got := intake.MissingInfoChaserDef().Actions; len(got) != 1 || got[0] != intake.ActionRequestMissingInfo {
		t.Fatalf("actions = %v, want [%s]", got, intake.ActionRequestMissingInfo)
	}
}

// fint03Run seeds the intake matter and a known contact named after short, then runs F-INT-03
// with a script that submits the updated lead fields under gateKind.
func fint03Run(t *testing.T, short, gateKind string) (*agenttest.FeatureDriver, *intbDrafts, agenttest.FeatureRun, []string) {
	t.Helper()
	fd, drafts := intbFeature(t, intake.MissingInfoChaserDef())
	m := fd.Seed.Matter("2026-0103", short)
	calls := intbCalls("Synth Lead", string(m.ID), "Updated lead fields: Synth Lead",
		"Filled: client contact matches an existing client record. Missing: adverse party (question: Who is the other side?); date of the event (question: When did it happen?).", gateKind)
	model := intbScript("F-INT-03", calls, "Drafted the updated lead fields for approval.")
	return fd, drafts, fd.Fire(context.Background(), model, "run-fint03", "event: intake.lead_awaiting_info"), intbToolNames(calls)
}

// rule: ARC-I-035
// rule: ARC-I-039
func TestFINT03_FromFeature(t *testing.T) {
	fd, drafts, run, _ := fint03Run(t, "Synth Lead", intbSkillAutonomy)
	intbCheckRun(t, fd, drafts, run, intbSkillAutonomy)
}

// rule: ARC-I-035
// platform.skill_autonomy is a tool-gap kind: draft_document then submit_for_approval opens
// it, and submit_for_approval may open no kind outside the Def.
func TestFINT03_GateKindNotAllowed(t *testing.T) {
	for _, kind := range []string{intbLeadDecision, "conflict.clearance", "platform.outgoing_email", "trust.withdrawal"} {
		t.Run(kind, func(t *testing.T) {
			_, drafts, run, _ := fint03Run(t, "Synth Lead", kind)
			intbCheckKindRefused(t, drafts, run)
		})
	}
}

// rule: ARC-I-038
func TestFINT03_Injection(t *testing.T) {
	fd, _, run, want := fint03Run(t, "Synth Lead "+intbInjected, intbSkillAutonomy)
	intbCheckInjection(t, fd, run, want, intbInjected)
}
