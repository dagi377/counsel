package access_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
)

// rule: AI-R-003
func TestFACC13_DefMatchesSpec(t *testing.T) {
	accdCheckDefMatchesSpec(t, access.SummarizeSessionDef())
}

// rule: AI-R-005
func TestFACC13_Prompt(t *testing.T) {
	accdCheckPrompt(t, access.SummarizeSessionDef())
}

// The ok fixture pins the prompt hash, so a prompt or tool change is noticed.
func TestFACC13_FixtureHash(t *testing.T) {
	accdCheckFixtureHash(t, access.SummarizeSessionDef())
}

// F-ACC-13 runs only from its screen action, support-access > Summarize session, on firm records.
func TestFACC13_TriggerBinding(t *testing.T) {
	accdCheckScreenAction(t, access.SummarizeSessionDef(), "support-access > Summarize session", "admin.support_access")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFACC13_FromFeature(t *testing.T) {
	accdCheckReadOnly(t, access.SummarizeSessionDef(), "support-access > Summarize session",
		"Support viewed 2 matters you can see, 2026-0711 and 2026-0712; no change shows in the search.")
}

// rule: AI-R-005
// rule: ARC-I-036
func TestFACC13_ScreenActionRoute(t *testing.T) {
	accdCheckRoute(t, access.SummarizeSessionDef(), "support-access > Summarize session", "admin.support_access",
		"Support viewed 2 matters you can see, 2026-0711 and 2026-0712; no change shows in the search.")
}

// rule: AI-R-003
// rule: ARC-I-039
func TestFACC13_AgentNeverApproves(t *testing.T) {
	accdCheckNoApproval(t, access.SummarizeSessionDef(), "core.vendor_support")
}
