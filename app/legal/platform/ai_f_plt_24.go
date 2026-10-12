package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionExplainChange is the firm-insights screen action that runs F-PLT-24.
const ActionExplainChange = "firm-insights > Explain change"

//go:embed prompts/f-plt-24.md
var promptFPLT24 embed.FS

// ExplainChangeDef is F-PLT-24: on the firm-insights screen a user asks why a number changed.
// The agent names the matters and users that moved the number, from the records it reads. It
// is read-only: no write tool, no gate, and the answer goes back to the screen-action caller
// only, never stored (AI-R-005).
func ExplainChangeDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-24",
		Area:    "platform",
		Name:    "Explain change",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionExplainChange,
		Actions: []string{ActionExplainChange},
		Kind:    ai.KindChat,
		Tools:   []string{agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView},
		Prompt:     promptFPLT24,
		PromptPath: "prompts/f-plt-24.md",
		Fixture:    "testdata/ai/f-plt-24/explain-change",
	}
}
