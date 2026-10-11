package conflict_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/conflict"
)

// rule: AI-R-003
func TestFCON17_DefMatchesSpec(t *testing.T) {
	conCheckScreenSpec(t, conflict.EstimateImpactDef(), conflict.ActionEstimateImpact)
}

// rule: AI-R-005
func TestFCON17_Prompt(t *testing.T) {
	conCheckReadOnlyPrompt(t, conflict.EstimateImpactDef(), "estimate how the proposed search settings would change", "shows hits gained/lost")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFCON17_FromFeature(t *testing.T) {
	conCheckReadOnly(t, conflict.EstimateImpactDef(), "The proposed settings add one hit on last month's check and lose none.")
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFCON17_AgentNeverApproves(t *testing.T) {
	conCheckNoApproval(t, conflict.EstimateImpactDef())
}
