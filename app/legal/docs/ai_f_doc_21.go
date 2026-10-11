package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionFollowUpQuestion is the multi-document-analysis action that runs F-DOC-21 on request.
const ActionFollowUpQuestion = "multi-document-analysis > Follow-up question"

//go:embed prompts/f-doc-21.md
var promptFDOC21 embed.FS

// FollowUpQuestionDef is F-DOC-21: on the multi-document-analysis screen the agent answers a
// follow-up question on the same selected documents, citing each fact. The session keeps the
// thread on the screen; the run stores nothing.
func FollowUpQuestionDef() feature.Def {
	return screenAnswerDef("F-DOC-21", "Follow-up question", ActionFollowUpQuestion,
		promptFDOC21, "prompts/f-doc-21.md", "testdata/ai/f-doc-21/follow-up-question")
}
