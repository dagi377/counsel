package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionAnswerWithCitations is the ask-agent screen action that runs F-PLT-18.
const ActionAnswerWithCitations = "ask-agent > Answer with citations"

//go:embed prompts/f-plt-18.md
var promptFPLT18 embed.FS

// AnswerWithCitationsDef is F-PLT-18: on the ask-agent screen a user asks a question about the
// selected matter. The agent reads the matter and its documents and answers with a citation
// for every fact. The answer is internal: nothing leaves the firm from this screen. It is
// read-only: no write tool, no gate, and the answer goes back to the screen-action caller
// only, never stored (AI-R-005).
func AnswerWithCitationsDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-18",
		Area:    "platform",
		Name:    "Answer with citations",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionAnswerWithCitations,
		Actions: []string{ActionAnswerWithCitations},
		Kind:    ai.KindChat,
		Tools:   []string{agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView},
		Prompt:     promptFPLT18,
		PromptPath: "prompts/f-plt-18.md",
		Fixture:    "testdata/ai/f-plt-18/answer-with-citations",
	}
}
