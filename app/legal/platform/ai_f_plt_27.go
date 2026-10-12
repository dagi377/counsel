package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionResearch is the research-workspace screen action that runs F-PLT-27.
const ActionResearch = "research-workspace > Research"

//go:embed prompts/f-plt-27.md
var promptFPLT27 embed.FS

// ResearchDef is F-PLT-27: in the research workspace a user asks a research question on the
// selected matter. The agent reads the matter and its documents and answers with a citation
// for every point. It is read-only: no write tool, no gate, and the answer goes back to the
// screen-action caller only, never stored (AI-R-005).
func ResearchDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-27",
		Area:    "platform",
		Name:    "Research",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionResearch,
		Actions: []string{ActionResearch},
		Kind:    ai.KindChat,
		Tools:   []string{agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView},
		Prompt:     promptFPLT27,
		PromptPath: "prompts/f-plt-27.md",
		Fixture:    "testdata/ai/f-plt-27/research",
	}
}
