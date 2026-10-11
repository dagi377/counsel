package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionSuggestScoringWeights is the intake-settings screen action that runs F-INT-16.
const ActionSuggestScoringWeights = "intake-settings > Suggest scoring weights"

//go:embed prompts/f-int-16.md
var promptFINT16 embed.FS

// SuggestScoringWeightsDef is F-INT-16: on the intake-settings screen the agent suggests lead
// scoring weights from the last 12 months of accepted and lost leads. It is read-only: no
// write tool, no gate. The answer goes back through the screen-action route only and is never
// stored (AI-R-005); a person applies the weights.
func SuggestScoringWeightsDef() feature.Def {
	return feature.Def{
		ID:         "F-INT-16",
		Area:       "intake",
		Name:       "Suggest scoring weights",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionSuggestScoringWeights,
		Actions:    []string{ActionSuggestScoringWeights},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), inthAnswerTools...),
		Grant:      append([]string(nil), inthAnswerGrant...),
		Prompt:     promptFINT16,
		PromptPath: "prompts/f-int-16.md",
		Fixture:    "testdata/ai/f-int-16/suggest-scoring-weights",
	}
}
