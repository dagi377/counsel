package intake_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/intake"
)

// rule: AI-R-003
func TestFINT20_DefMatchesSpec(t *testing.T) {
	intgCheckDefMatchesSpec(t, intake.MapAnswersToLeadDef())
}

// rule: AI-R-005
func TestFINT20_Prompt(t *testing.T) {
	intgCheckPrompt(t, intake.MapAnswersToLeadDef(), "Map answers to lead", "map answers to lead on intake-questionnaire-send")
}

// F-INT-20 runs only on request from the intake-questionnaire-send screen.
func TestFINT20_TriggerBinding(t *testing.T) {
	d := intake.MapAnswersToLeadDef()
	if got := d.Actions; len(got) != 1 || got[0] != intake.ActionMapAnswersToLead {
		t.Fatalf("actions = %v, want [%s]", got, intake.ActionMapAnswersToLead)
	}
	if d.Trigger != "on request: "+intake.ActionMapAnswersToLead {
		t.Fatalf("trigger = %q", d.Trigger)
	}
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
// The result is a ranked list of matches and flag rows, each cited to a record the run's
// searches returned.
func TestFINT20_FromFeature(t *testing.T) {
	intgCheckReadOnly(t, intake.MapAnswersToLeadDef(), "Ranked matches: 1. the existing client contact (same name). Flags: none.")
}

// rule: AI-R-003
func TestFINT20_AgentNeverApproves(t *testing.T) {
	intgCheckNoApproval(t, intake.MapAnswersToLeadDef())
}
