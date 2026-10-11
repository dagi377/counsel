package intake_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/intake"
)

// rule: AI-R-003
func TestFINT09_DefMatchesSpec(t *testing.T) {
	intgCheckDefMatchesSpec(t, intake.WeeklyIntakeDigestDef())
}

// rule: AI-R-005
func TestFINT09_Prompt(t *testing.T) {
	intgCheckPrompt(t, intake.WeeklyIntakeDigestDef(), "Weekly intake digest", "Internal digest (no approval)")
}

// F-INT-09 runs on the first business day of each week for its designated lawyer
// (JOB-ACTOR), and on request from the intake dashboard.
func TestFINT09_TriggerBinding(t *testing.T) {
	b, err := intake.WeeklyIntakeDigestBinding()
	intgCheckDesignated(t, b, err, "F-INT-09", trigger.CadenceWeekly)
	if got := intake.WeeklyIntakeDigestDef().Actions; len(got) != 1 || got[0] != intake.ActionWeeklyIntakeDigest {
		t.Fatalf("actions = %v, want [%s]", got, intake.ActionWeeklyIntakeDigest)
	}
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFINT09_FromFeature(t *testing.T) {
	intgCheckReadOnly(t, intake.WeeklyIntakeDigestDef(), "Last week: one new lead, none converted. Needs a lawyer: none.")
}

// rule: AI-R-003
func TestFINT09_AgentNeverApproves(t *testing.T) {
	intgCheckNoApproval(t, intake.WeeklyIntakeDigestDef())
}
