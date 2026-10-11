package access

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureExplainPattern is the spec id of "Explain pattern".
const FeatureExplainPattern = "F-ACC-11"

// explainPatternTrigger is how features.json words F-ACC-11's start: a screen action only.
const explainPatternTrigger = "on request: access-audit > Explain pattern"

//go:embed prompts/f-acc-11.md
var promptFACC11 embed.FS

// ExplainPatternDef is F-ACC-11: on the access-audit screen the agent summarizes what is
// unusual about the selected events, from the matters search_matters lets the acting user see.
// It is read-only: no write tool, no gate. The answer goes back through the screen-action
// route only and is never stored (AI-R-005). The answer cites the matters
// search_matters returned (matter:<id>).
func ExplainPatternDef() feature.Def {
	return feature.Def{
		ID:      FeatureExplainPattern,
		Area:    "access",
		Name:    "Explain pattern",
		Source:  feature.SourceScreenAction,
		Trigger: explainPatternTrigger,
		// The screen action is the trigger itself: "access-audit > Explain pattern".
		Actions: []string{strings.TrimPrefix(explainPatternTrigger, "on request: ")},
		Kind:    ai.KindChat,
		Tools:   []string{"search_matters"},
		// The run's agent principal holds the capability of its one read tool and no other.
		Grant:      []string{"matters.view"},
		Prompt:     promptFACC11,
		PromptPath: "prompts/f-acc-11.md",
		Fixture:    "testdata/ai/f-acc-11/explain-pattern",
	}
}
