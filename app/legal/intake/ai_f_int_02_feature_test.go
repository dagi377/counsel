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
func TestFINT02_DefMatchesSpec(t *testing.T) {
	intbCheckDefMatchesSpec(t, intake.MailboxWatcherDef())
}

// rule: AI-R-005
func TestFINT02_Prompt(t *testing.T) {
	intbCheckPrompt(t, intake.MailboxWatcherDef(),
		"Intake mailbox watcher", "A draft lead for staff review", intbLeadDecision, "report_plan", "draft_document", "submit_for_approval")
}

// F-INT-02 starts on arriving mail, acting for the event's user, and on request from the
// phone-and-email-capture screen.
func TestFINT02_TriggerBinding(t *testing.T) {
	b, err := intake.MailboxWatcherBinding()
	intbCheckBinding(t, b, err, "F-INT-02", []string{intake.EventEmailReceived}, trigger.CadenceNone, false)
	if got := intake.MailboxWatcherDef().Actions; len(got) != 1 || got[0] != intake.ActionExtractLead {
		t.Fatalf("actions = %v, want [%s]", got, intake.ActionExtractLead)
	}
}

// fint02Run seeds the intake matter and a known contact named after short, then runs F-INT-02
// with a script that submits the draft lead under gateKind.
func fint02Run(t *testing.T, short, gateKind string) (*agenttest.FeatureDriver, *intbDrafts, agenttest.FeatureRun, []string) {
	t.Helper()
	fd, drafts := intbFeature(t, intake.MailboxWatcherDef())
	m := fd.Seed.Matter("2026-0101", short)
	calls := intbCalls("Synth Lead", string(m.ID), "Draft lead: Synth Lead",
		"Prospective client: Synth Lead (existing contact). Matter kind: contract dispute. Contact: by reply to the intake email.", gateKind)
	model := intbScript("F-INT-02", calls, "Drafted the lead for staff review.")
	return fd, drafts, fd.Fire(context.Background(), model, "run-fint02", "event: email.received to the intake address"), intbToolNames(calls)
}

// rule: ARC-I-035
// rule: ARC-I-039
func TestFINT02_FromFeature(t *testing.T) {
	fd, drafts, run, _ := fint02Run(t, "Synth Lead", intbLeadDecision)
	intbCheckRun(t, fd, drafts, run, intbLeadDecision)
}

// rule: ARC-I-035
// intake.lead_decision is a tool-gap kind: draft_document then submit_for_approval opens it,
// and submit_for_approval may open no kind outside the Def.
func TestFINT02_GateKindNotAllowed(t *testing.T) {
	for _, kind := range []string{intbSkillAutonomy, "conflict.clearance", "matter.opening", "trust.withdrawal"} {
		t.Run(kind, func(t *testing.T) {
			_, drafts, run, _ := fint02Run(t, "Synth Lead", kind)
			intbCheckKindRefused(t, drafts, run)
		})
	}
}

// rule: ARC-I-038
func TestFINT02_Injection(t *testing.T) {
	fd, _, run, want := fint02Run(t, "Synth Lead "+intbInjected, intbLeadDecision)
	intbCheckInjection(t, fd, run, want, intbInjected)
}
