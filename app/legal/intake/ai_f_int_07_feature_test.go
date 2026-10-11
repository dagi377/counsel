package intake_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/intake"
)

// rule: AI-R-003
func TestFINT07_DefMatchesSpec(t *testing.T) {
	intgCheckDefMatchesSpec(t, intake.PipelineFollowUpsDef())
}

// rule: AI-R-005
func TestFINT07_Prompt(t *testing.T) {
	intgCheckPrompt(t, intake.PipelineFollowUpsDef(), "Pipeline follow-ups", "Sent nudges and a stuck list in the digest")
}

// F-INT-07 runs every business hour for its designated lawyer (JOB-ACTOR).
func TestFINT07_TriggerBinding(t *testing.T) {
	b, err := intake.PipelineFollowUpsBinding()
	intgCheckDesignated(t, b, err, "F-INT-07", trigger.CadenceHourly)
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFINT07_FromFeature(t *testing.T) {
	intgCheckReadOnly(t, intake.PipelineFollowUpsDef(), "Nudge due: one lead with no reply. Stuck: none.")
}

// rule: AI-R-003
func TestFINT07_AgentNeverApproves(t *testing.T) {
	intgCheckNoApproval(t, intake.PipelineFollowUpsDef())
}
