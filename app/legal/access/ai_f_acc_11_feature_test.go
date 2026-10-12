package access_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
)

// rule: AI-R-003
func TestFACC11_DefMatchesSpec(t *testing.T) {
	accdCheckDefMatchesSpec(t, access.ExplainPatternDef())
}

// rule: AI-R-005
func TestFACC11_Prompt(t *testing.T) {
	accdCheckPrompt(t, access.ExplainPatternDef())
}

// The ok fixture pins the prompt hash, so a prompt or tool change is noticed.
func TestFACC11_FixtureHash(t *testing.T) {
	accdCheckFixtureHash(t, access.ExplainPatternDef())
}

// F-ACC-11 runs only from its screen action, access-audit > Explain pattern, on firm records.
func TestFACC11_TriggerBinding(t *testing.T) {
	accdCheckScreenAction(t, access.ExplainPatternDef(), "access-audit > Explain pattern", "admin.audit_log")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFACC11_FromFeature(t *testing.T) {
	accdCheckReadOnly(t, access.ExplainPatternDef(), "access-audit > Explain pattern",
		"The selected events touch 2 matters you can see, 2026-0711 and 2026-0712.")
}

// rule: AI-R-005
// rule: ARC-I-036
func TestFACC11_ScreenActionRoute(t *testing.T) {
	accdCheckRoute(t, access.ExplainPatternDef(), "access-audit > Explain pattern", "admin.audit_log",
		"The selected events touch 2 matters you can see, 2026-0711 and 2026-0712.")
}

// rule: AI-R-003
// rule: ARC-I-039
func TestFACC11_AgentNeverApproves(t *testing.T) {
	accdCheckNoApproval(t, access.ExplainPatternDef(), "core.matter_visibility")
}
