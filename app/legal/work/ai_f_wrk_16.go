package work

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureSuggestCategories is the spec id of "Suggest categories".
const FeatureSuggestCategories = "F-WRK-16"

// suggestCategoriesTrigger is how features.json words F-WRK-16's start: a screen action only.
const suggestCategoriesTrigger = "on request: work-categories > Suggest categories"

//go:embed prompts/f-wrk-16.md
var promptFWRK16 embed.FS

// SuggestCategoriesDef is F-WRK-16: On the work-categories screen the agent reads the selected matters' work item titles and suggests categories for them. It is read-only: no write tool, no gate, and the answer
// goes back to the screen-action caller only, never stored (AI-R-005).
func SuggestCategoriesDef() feature.Def {
	return feature.Def{
		ID:      FeatureSuggestCategories,
		Area:    "work",
		Name:    "Suggest categories",
		Source:  feature.SourceScreenAction,
		Trigger: suggestCategoriesTrigger,
		// The screen action is the trigger itself: "work-categories > Suggest categories".
		Actions: []string{strings.TrimPrefix(suggestCategoriesTrigger, "on request: ")},
		Kind:    ai.KindChat,
		Tools:   []string{"get_matter", "list_work_items", "list_documents", "read_document"},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{"documents.view", "matters.view", "work.view"},
		Prompt:     promptFWRK16,
		PromptPath: "prompts/f-wrk-16.md",
		Fixture:    "testdata/ai/f-wrk-16/suggest-categories",
	}
}
