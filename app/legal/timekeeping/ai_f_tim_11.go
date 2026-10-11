package timekeeping

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-tim-11.md
var promptFTIM11 embed.FS

// SuggestCategoriesDef is F-TIM-11: on the activity-categories screen the agent clusters past
// narratives into proposed activity categories, each cluster cited to the entries behind it. It is read-only: no write tool, no gate, and the answer goes back
// to the screen-action caller only, never stored (AI-R-005).
func SuggestCategoriesDef() feature.Def {
	return feature.Def{
		ID:      "F-TIM-11",
		Area:    "time",
		Name:    "Suggest categories from history",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: activity-categories > Suggest categories from history",
		Actions: []string{"activity-categories > Suggest categories from history"},
		Kind:    ai.KindChat,
		// The a2 read tools; package agent imports this one, so its constants cannot be used.
		Tools: []string{"get_matter", "list_unbilled", "list_work_items"},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{"billing.view", "matters.view", "work.view"},
		Prompt:     promptFTIM11,
		PromptPath: "prompts/f-tim-11.md",
		Fixture:    "testdata/ai/f-tim-11/suggest-categories-from-history",
	}
}
