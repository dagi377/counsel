package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionAnswerWithCitations is the document-search action that runs F-DOC-14 on request.
const ActionAnswerWithCitations = "document-search > Answer with citations"

// screenAnswerTools are the read tools of the read-only document screen answers (F-DOC-14,
// F-DOC-20 to F-DOC-22), and screenAnswerGrant the capabilities their runs hold: each tool's
// a2 capability and nothing more.
var (
	screenAnswerTools = []string{"get_matter", "list_documents", "read_document", "search_documents"}
	screenAnswerGrant = []string{"matters.view", "documents.view"}
)

//go:embed prompts/f-doc-14.md
var promptFDOC14 embed.FS

// screenAnswerDef is a read-only chat Def run from one screen action: the four document read
// tools, no write tool and no gate kind, so the answer goes back to the screen-action route
// only and is never stored (AI-R-005).
func screenAnswerDef(id, name, action string, prompt embed.FS, promptPath, fixture string) feature.Def {
	return feature.Def{
		ID:         id,
		Area:       "documents",
		Name:       name,
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + action,
		Actions:    []string{action},
		Kind:       ai.KindChat,
		Tools:      append([]string(nil), screenAnswerTools...),
		Grant:      append([]string(nil), screenAnswerGrant...),
		Prompt:     prompt,
		PromptPath: promptPath,
		Fixture:    fixture,
	}
}

// AnswerWithCitationsDef is F-DOC-14: on the document-search screen the agent answers the
// user's question only from the search hits, cites each one, and says so when no hit supports
// an answer.
func AnswerWithCitationsDef() feature.Def {
	return screenAnswerDef("F-DOC-14", "Answer with citations", ActionAnswerWithCitations,
		promptFDOC14, "prompts/f-doc-14.md", "testdata/ai/f-doc-14/answer-with-citations")
}
