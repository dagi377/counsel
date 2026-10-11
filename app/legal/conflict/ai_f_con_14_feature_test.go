package conflict_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/conflict"
)

// rule: AI-R-003
func TestFCON14_DefMatchesSpec(t *testing.T) {
	conCheckScreenSpec(t, conflict.KYCSummaryDef(), conflict.ActionShowKYCSummary)
}

// rule: AI-R-005
func TestFCON14_Prompt(t *testing.T) {
	conCheckReadOnlyPrompt(t, conflict.KYCSummaryDef(), "summarize the client identification", "Card on the matter with status, check dates and risk rating")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFCON14_FromFeature(t *testing.T) {
	conCheckReadOnly(t, conflict.KYCSummaryDef(), "Client identified and verified; risk rating low.")
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFCON14_AgentNeverApproves(t *testing.T) {
	conCheckNoApproval(t, conflict.KYCSummaryDef())
}
