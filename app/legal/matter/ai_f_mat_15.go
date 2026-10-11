package matter

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureProposeNextActions is the spec id of "Propose next actions".
const FeatureProposeNextActions = "F-MAT-15"

// proposenextactionsTrigger is how features.json words F-MAT-15's start: a screen action only.
const proposenextactionsTrigger = "on request: matter-list > Propose next actions"

//go:embed prompts/f-mat-15.md
var promptFMAT15 embed.FS

// ProposeNextActionsDef is F-MAT-15: on the matter-list screen the agent reads the selected matters and proposes next actions, each with a citation. It is read-only: no write tool, no gate, and the answer goes back
// to the screen-action caller only, never stored (AI-R-005).
func ProposeNextActionsDef() feature.Def {
	return feature.Def{
		ID:      FeatureProposeNextActions,
		Area:    "matters",
		Name:    "Propose next actions",
		Source:  feature.SourceScreenAction,
		Trigger: proposenextactionsTrigger,
		// The screen action is the trigger itself: "matter-list > Propose next actions".
		Actions: []string{strings.TrimPrefix(proposenextactionsTrigger, "on request: ")},
		Kind:    ai.KindChat,
		Tools:   []string{"get_matter", "search_documents", "read_document", "search_contacts"},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{"contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFMAT15,
		PromptPath: "prompts/f-mat-15.md",
		Fixture:    "testdata/ai/f-mat-15/propose-next-actions",
	}
}
