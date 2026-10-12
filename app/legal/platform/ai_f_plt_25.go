package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionSummarizeThread is the internal-messages screen action that runs F-PLT-25.
const ActionSummarizeThread = "internal-messages > Summarize thread"

//go:embed prompts/f-plt-25.md
var promptFPLT25 embed.FS

// SummarizeThreadDef is F-PLT-25: on the internal-messages screen a user asks for a summary of
// the selected thread on a matter. The agent reads the matter and the thread and summarizes it
// with citations. It is read-only: no write tool, no gate, and the answer goes back to the
// screen-action caller only, never stored (AI-R-005).
func SummarizeThreadDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-25",
		Area:    "platform",
		Name:    "Summarize thread",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionSummarizeThread,
		Actions: []string{ActionSummarizeThread},
		Kind:    ai.KindChat,
		Tools:   []string{agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView},
		Prompt:     promptFPLT25,
		PromptPath: "prompts/f-plt-25.md",
		Fixture:    "testdata/ai/f-plt-25/summarize-thread",
	}
}
