package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionAskPlainLanguage is the screen action that runs F-WRK-13 on request.
const ActionAskPlainLanguage = "saved-filters > Ask in plain language"

//go:embed prompts/f-wrk-13.md
var promptFWRK13 embed.FS

// PlainLanguageFilterDef is F-WRK-13: on the saved-filters screen the agent turns a sentence
// into a work item query that the user sees and can edit before running it. It is read-only:
// no write tool, no gate. The answer goes back through the screen-action route only and is
// never stored (AI-R-005).
func PlainLanguageFilterDef() feature.Def {
	return feature.Def{
		ID:         "F-WRK-13",
		Area:       "work",
		Name:       "Ask in plain language",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionAskPlainLanguage,
		Actions:    []string{ActionAskPlainLanguage},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), wrkdReadTools...),
		Grant:      append([]string(nil), wrkdReadGrant...),
		Prompt:     promptFWRK13,
		PromptPath: "prompts/f-wrk-13.md",
		Fixture:    "testdata/ai/f-wrk-13/ask-in-plain-language",
	}
}
