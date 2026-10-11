package intake_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/intake"
)

// rule: AI-R-003
func TestFINT16_DefMatchesSpec(t *testing.T) {
	inthCheckScreenSpec(t, intake.SuggestScoringWeightsDef(), intake.ActionSuggestScoringWeights)
}

// rule: AI-R-005
func TestFINT16_Prompt(t *testing.T) {
	inthCheckPrompt(t, intake.SuggestScoringWeightsDef(), "suggest lead scoring weights", "a person applies them")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFINT16_FromFeature(t *testing.T) {
	inthCheckReadOnly(t, intake.SuggestScoringWeightsDef(), "Weights: practice area 40, urgency 30, source 20, limitation flag 10.")
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFINT16_AgentNeverApproves(t *testing.T) {
	inthCheckNoApproval(t, intake.SuggestScoringWeightsDef())
}
