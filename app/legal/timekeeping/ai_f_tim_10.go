package timekeeping

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-tim-10.md
var promptFTIM10 embed.FS

// FindInconsistenciesDef is F-TIM-10: on the activity-list screen the agent flags duplicate
// narratives, entries on closed matters and days over ten hours, ranked, each row cited to
// its time entry. Narratives are compared by embedding. It is read-only: no write tool, no
// gate, and the flags go back to the screen-action caller only, never stored (AI-R-005).
// A person fixes any entry.
func FindInconsistenciesDef() feature.Def {
	return feature.Def{
		ID:      "F-TIM-10",
		Area:    "time",
		Name:    "Find inconsistencies",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: activity-list > Find inconsistencies",
		Actions: []string{"activity-list > Find inconsistencies"},
		Kind:    ai.KindEmbedding,
		// The a2 read tools; package agent imports this one, so its constants cannot be used.
		Tools: []string{"get_matter", "list_unbilled", "list_work_items"},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{"billing.view", "matters.view", "work.view"},
		Prompt:     promptFTIM10,
		PromptPath: "prompts/f-tim-10.md",
		Fixture:    "testdata/ai/f-tim-10/find-inconsistencies",
	}
}
