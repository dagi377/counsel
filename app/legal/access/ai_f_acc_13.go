package access

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureSummarizeSession is the spec id of "Summarize session".
const FeatureSummarizeSession = "F-ACC-13"

// summarizeSessionTrigger is how features.json words F-ACC-13's start: a screen action only.
const summarizeSessionTrigger = "on request: support-access > Summarize session"

//go:embed prompts/f-acc-13.md
var promptFACC13 embed.FS

// SummarizeSessionDef is F-ACC-13: on the support-access screen the agent lists in plain
// language what vendor support viewed or changed in the selected session, from the matters
// search_matters lets the acting user see.
// It is read-only: no write tool, no gate. The answer goes back through the screen-action
// route only and is never stored (AI-R-005). search_matters returns no document source, so the
// answer may carry no citation; any citation it does carry is still checked.
func SummarizeSessionDef() feature.Def {
	return feature.Def{
		ID:      FeatureSummarizeSession,
		Area:    "access",
		Name:    "Summarize session",
		Source:  feature.SourceScreenAction,
		Trigger: summarizeSessionTrigger,
		// The screen action is the trigger itself: "support-access > Summarize session".
		Actions: []string{strings.TrimPrefix(summarizeSessionTrigger, "on request: ")},
		Kind:    ai.KindChat,
		Tools:   []string{"search_matters"},
		// The run's agent principal holds the capability of its one read tool and no other.
		Grant:              []string{"matters.view"},
		Prompt:             promptFACC13,
		PromptPath:         "prompts/f-acc-13.md",
		Fixture:            "testdata/ai/f-acc-13/summarize-session",
		AnswerMayBeUncited: true,
	}
}
