package matter_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/matter"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFMAT16_DefMatchesSpec(t *testing.T) {
	mateCheckDefMatchesSpec(t, matter.RerunConflictDef())
}

func TestFMAT16_Prompt(t *testing.T) {
	mateCheckPrompt(t, matter.RerunConflictDef())
}

// F-MAT-16 starts only on request from the matter-open wizard.
func TestFMAT16_TriggerBinding(t *testing.T) {
	mateCheckTrigger(t, matter.RerunConflictDef(), "matter-open-wizard > Re-run conflict on new parties")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFMAT16_FromFeature(t *testing.T) {
	mateCheckConflictRun(t, matter.RerunConflictDef(), "Northgate Rentals Inc.",
		"Northgate Rentals Inc. joins the matter as landlord of the premises.")
}
