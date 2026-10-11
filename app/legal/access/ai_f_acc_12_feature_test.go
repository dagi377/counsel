package access_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
)

// rule: AI-R-003
func TestFACC12_DefMatchesSpec(t *testing.T) {
	accdCheckDefMatchesSpec(t, access.ExplainChangeDef())
}

// rule: AI-R-005
func TestFACC12_Prompt(t *testing.T) {
	accdCheckPrompt(t, access.ExplainChangeDef())
}

// The ok fixture pins the prompt hash, so a prompt or tool change is noticed.
func TestFACC12_FixtureHash(t *testing.T) {
	accdCheckFixtureHash(t, access.ExplainChangeDef())
}

// F-ACC-12 runs only from its screen action, sign-in-settings > Explain change, on firm records.
func TestFACC12_TriggerBinding(t *testing.T) {
	accdCheckScreenAction(t, access.ExplainChangeDef(), "sign-in-settings > Explain change", "admin.security")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFACC12_FromFeature(t *testing.T) {
	accdCheckReadOnly(t, access.ExplainChangeDef(), "sign-in-settings > Explain change",
		"The draft reaches the people on 2 matters you can see, 2026-0711 and 2026-0712; none would be locked out.")
}

// rule: AI-R-005
// rule: ARC-I-036
func TestFACC12_ScreenActionRoute(t *testing.T) {
	accdCheckRoute(t, access.ExplainChangeDef(), "sign-in-settings > Explain change", "admin.security",
		"The draft reaches the people on 2 matters you can see, 2026-0711 and 2026-0712; none would be locked out.")
}

// rule: AI-R-003
// rule: ARC-I-039
func TestFACC12_AgentNeverApproves(t *testing.T) {
	accdCheckNoApproval(t, access.ExplainChangeDef(), "core.sign_in_settings")
}
