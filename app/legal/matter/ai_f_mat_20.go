package matter

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureSuggestFromPastMatters is the spec id of "Suggest from past matters".
const FeatureSuggestFromPastMatters = "F-MAT-20"

// suggestfrompastmattersTrigger is how features.json words F-MAT-20's start: a screen action only.
const suggestfrompastmattersTrigger = "on request: matter-templates > Suggest from past matters"

//go:embed prompts/f-mat-20.md
var promptFMAT20 embed.FS

// SuggestFromPastMattersDef is F-MAT-20: on the matter-templates screen the agent clusters the area's closed matters and proposes template presets, each with a citation. It is read-only: no write tool, no gate, and the answer goes back
// to the screen-action caller only, never stored (AI-R-005).
func SuggestFromPastMattersDef() feature.Def {
	return feature.Def{
		ID:      FeatureSuggestFromPastMatters,
		Area:    "matters",
		Name:    "Suggest from past matters",
		Source:  feature.SourceScreenAction,
		Trigger: suggestfrompastmattersTrigger,
		// The screen action is the trigger itself: "matter-templates > Suggest from past matters".
		Actions: []string{strings.TrimPrefix(suggestfrompastmattersTrigger, "on request: ")},
		Kind:    ai.KindChat,
		Tools:   []string{"get_matter", "search_documents", "read_document", "search_contacts"},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{"contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFMAT20,
		PromptPath: "prompts/f-mat-20.md",
		Fixture:    "testdata/ai/f-mat-20/suggest-from-past-matters",
	}
}
