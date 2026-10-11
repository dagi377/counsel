package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionSuggestReply is the mail-add-in screen action that runs F-PLT-26.
const ActionSuggestReply = "mail-add-in > Suggest reply"

//go:embed prompts/f-plt-26.md
var promptFPLT26 embed.FS

// SuggestReplyDef is F-PLT-26: in the mail add-in a user asks for a suggested reply to the
// open email on the selected matter. The agent reads the matter context and suggests reply
// text with the tone and length the user chose. Only the user inserts it into compose;
// substantive replies sent from the app still go through G-OUT. It is read-only: no write
// tool, no gate, and the answer goes back to the screen-action caller only, never stored
// (AI-R-005).
func SuggestReplyDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-26",
		Area:    "platform",
		Name:    "Suggest reply",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionSuggestReply,
		Actions: []string{ActionSuggestReply},
		Kind:    ai.KindChat,
		Tools:   []string{agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView},
		Prompt:     promptFPLT26,
		PromptPath: "prompts/f-plt-26.md",
		Fixture:    "testdata/ai/f-plt-26/suggest-reply",
	}
}
