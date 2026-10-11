package matter_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/matter"
)

// rule: AI-R-003
func TestFMAT22_DefMatchesSpec(t *testing.T) {
	mathCheckDefMatchesSpec(t, matter.SuggestCleanupDef())
}

// rule: AI-R-005
func TestFMAT22_Prompt(t *testing.T) {
	mathCheckPrompt(t, matter.SuggestCleanupDef())
}

// F-MAT-22 runs only from its screen action, contact-field-settings > Suggest cleanup.
func TestFMAT22_TriggerBinding(t *testing.T) {
	mathCheckScreenAction(t, matter.SuggestCleanupDef(), "contact-field-settings > Suggest cleanup")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFMAT22_FromFeature(t *testing.T) {
	mathCheckReadOnly(t, matter.SuggestCleanupDef(), mathReadOnlyCase{
		action: "contact-field-settings > Suggest cleanup",
		query:  "tag",
		lines: []string{
			"Contact tag register, reviewed quarterly.",
			"Tag Litigation and tag litigation-active both added 2026-10-01; field Fax unused since then.",
		},
		answer: "1. Near-duplicate tags: Litigation and litigation-active. 2. Unused field: Fax.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFMAT22_AgentNeverApproves(t *testing.T) {
	mathCheckNoApproval(t, matter.SuggestCleanupDef())
}
