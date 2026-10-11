package matter_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/matter"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFMAT18_DefMatchesSpec(t *testing.T) {
	mateCheckDefMatchesSpec(t, matter.ProposeRelationshipDef())
}

func TestFMAT18_Prompt(t *testing.T) {
	mateCheckPrompt(t, matter.ProposeRelationshipDef())
}

// F-MAT-18 starts only on request from the contact-detail screen.
func TestFMAT18_TriggerBinding(t *testing.T) {
	mateCheckTrigger(t, matter.ProposeRelationshipDef(), "contact-detail > Propose relationship")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFMAT18_FromFeature(t *testing.T) {
	mateCheckConflictRun(t, matter.ProposeRelationshipDef(), "Northgate Rentals Inc.",
		"Corporate profile: the contact is a director of Northgate Rentals Inc.")
}
