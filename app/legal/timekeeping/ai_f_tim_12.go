package timekeeping

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-tim-12.md
var promptFTIM12 embed.FS

// ExplainShortfallDef is F-TIM-12: on the timekeeper-targets screen the agent explains why a timekeeper
// is short of target, from the time and open work the run reads, each figure cited to its time
// entry. It is read-only: no write tool, no gate, and the answer goes back to the
// screen-action caller only, never stored (AI-R-005).
func ExplainShortfallDef() feature.Def {
	return feature.Def{
		ID:      "F-TIM-12",
		Area:    "time",
		Name:    "Explain shortfall",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: timekeeper-targets > Explain shortfall",
		Actions: []string{"timekeeper-targets > Explain shortfall"},
		Kind:    ai.KindChat,
		// The a2 read tools; package agent imports this one, so its constants cannot be used.
		Tools: []string{"get_matter", "list_unbilled", "list_work_items"},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{"billing.view", "matters.view", "work.view"},
		Prompt:     promptFTIM12,
		PromptPath: "prompts/f-tim-12.md",
		Fixture:    "testdata/ai/f-tim-12/explain-shortfall",
	}
}
