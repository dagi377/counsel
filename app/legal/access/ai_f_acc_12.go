package access

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureExplainChange is the spec id of "Explain change".
const FeatureExplainChange = "F-ACC-12"

// explainChangeTrigger is how features.json words F-ACC-12's start: a screen action only.
const explainChangeTrigger = "on request: sign-in-settings > Explain change"

//go:embed prompts/f-acc-12.md
var promptFACC12 embed.FS

// ExplainChangeDef is F-ACC-12: on the sign-in-settings screen the agent states who the draft
// sign-in policy would lock out or force to re-enrol, from what search_matters returns.
// It is read-only: no write tool, no gate. The answer goes back through the screen-action
// route only and is never stored (AI-R-005). search_matters returns no document source, so the
// answer may carry no citation; any citation it does carry is still checked.
func ExplainChangeDef() feature.Def {
	return feature.Def{
		ID:      FeatureExplainChange,
		Area:    "access",
		Name:    "Explain change",
		Source:  feature.SourceScreenAction,
		Trigger: explainChangeTrigger,
		// The screen action is the trigger itself: "sign-in-settings > Explain change".
		Actions: []string{strings.TrimPrefix(explainChangeTrigger, "on request: ")},
		Kind:    ai.KindChat,
		Tools:   []string{"search_matters"},
		// The run's agent principal holds the capability of its one read tool and no other.
		Grant:              []string{"matters.view"},
		Prompt:             promptFACC12,
		PromptPath:         "prompts/f-acc-12.md",
		Fixture:            "testdata/ai/f-acc-12/explain-change",
		AnswerMayBeUncited: true,
	}
}
