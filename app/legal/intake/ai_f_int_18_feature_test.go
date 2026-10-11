package intake_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/intake"
)

// rule: AI-R-003
func TestFINT18_DefMatchesSpec(t *testing.T) {
	inthCheckScreenSpec(t, intake.DryRunLastLeadsDef(), intake.ActionDryRunLastLeads)
}

// rule: AI-R-005
func TestFINT18_Prompt(t *testing.T) {
	inthCheckPrompt(t, intake.DryRunLastLeadsDef(), "dry-run the selected automation on the last 20 leads", "shows who would have got what")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFINT18_FromFeature(t *testing.T) {
	inthCheckReadOnly(t, intake.DryRunLastLeadsDef(), "1 of 1 leads would have got the welcome email.")
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFINT18_AgentNeverApproves(t *testing.T) {
	inthCheckNoApproval(t, intake.DryRunLastLeadsDef())
}
