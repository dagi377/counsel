package timekeeping

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-tim-09.md
var promptFTIM09 embed.FS

// ExplainTrendDef is F-TIM-09: on the utilization report the agent explains the trend the user
// selected in a short narrative whose figures come only from the report's reads, each cited. It is read-only: no write tool, no gate, and the answer goes back
// to the screen-action caller only, never stored (AI-R-005).
func ExplainTrendDef() feature.Def {
	return feature.Def{
		ID:      "F-TIM-09",
		Area:    "time",
		Name:    "Explain trend",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: utilization > Explain trend",
		Actions: []string{"utilization > Explain trend"},
		Kind:    ai.KindChat,
		// The a2 read tools; package agent imports this one, so its constants cannot be used.
		Tools: []string{"get_matter", "list_unbilled", "list_work_items"},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{"billing.view", "matters.view", "work.view"},
		Prompt:     promptFTIM09,
		PromptPath: "prompts/f-tim-09.md",
		Fixture:    "testdata/ai/f-tim-09/explain-trend",
	}
}
