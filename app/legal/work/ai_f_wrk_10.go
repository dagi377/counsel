package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionAskQuestion is the screen action that runs F-WRK-10 on request.
const ActionAskQuestion = "work-item > Ask a question"

//go:embed prompts/f-wrk-10.md
var promptFWRK10 embed.FS

// AskQuestionDef is F-WRK-10: on a work item the agent drafts the question it needs a person
// to answer, addressed to the item's assignee or approver, from the item, its matter and
// documents. It is read-only: no write tool, no gate. The answer goes back through the
// screen-action route only and is never stored (AI-R-005).
func AskQuestionDef() feature.Def {
	return feature.Def{
		ID:         "F-WRK-10",
		Area:       "work",
		Name:       "Ask a question",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionAskQuestion,
		Actions:    []string{ActionAskQuestion},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), wrkdReadTools...),
		Grant:      append([]string(nil), wrkdReadGrant...),
		Prompt:     promptFWRK10,
		PromptPath: "prompts/f-wrk-10.md",
		Fixture:    "testdata/ai/f-wrk-10/ask-a-question",
	}
}
