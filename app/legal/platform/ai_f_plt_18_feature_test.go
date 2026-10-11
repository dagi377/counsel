package platform_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/platform"
)

// rule: AI-R-003
func TestFPLT18_DefMatchesSpec(t *testing.T) {
	pltfCheckDefMatchesSpec(t, platform.AnswerWithCitationsDef())
}

// rule: AI-R-005
func TestFPLT18_Prompt(t *testing.T) {
	pltjCheckPrompt(t, platform.AnswerWithCitationsDef())
}

// F-PLT-18 runs only from its screen action, ask-agent > Answer with citations.
func TestFPLT18_TriggerBinding(t *testing.T) {
	pltjCheckScreenAction(t, platform.AnswerWithCitationsDef(), "ask-agent > Answer with citations")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFPLT18_FromFeature(t *testing.T) {
	pltjCheckReadOnly(t, platform.AnswerWithCitationsDef(), pltjReadOnlyCase{
		action: platform.ActionAnswerWithCitations,
		lines: []string{
			"Lease between Synth Holdings and the tenant.",
			"The lease term ends 2026-10-01 unless renewed in writing.",
		},
		answer: "The lease term ends unless it is renewed in writing.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFPLT18_AgentNeverApproves(t *testing.T) {
	pltjCheckNoApproval(t, platform.AnswerWithCitationsDef())
}
