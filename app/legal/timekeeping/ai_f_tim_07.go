package timekeeping

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionRewriteNarrative is the time-entry-detail screen action that runs F-TIM-07.
const ActionRewriteNarrative = "time-entry-detail > Rewrite narrative"

//go:embed prompts/f-tim-07.md
var promptFTIM07 embed.FS

// RewriteNarrativeDef is F-TIM-07: on the time-entry-detail screen, the agent reads the
// entry's matter, its unbilled time and its tasks and offers a clearer narrative as a
// suggested entry through propose_time_entries, which opens timekeeping.accept (G9) with the
// acting user as preparer. The entry's owner accepts it; the agent never does. A screen action
// starts it through the screen-action route, acting for the user who asked.
func RewriteNarrativeDef() feature.Def {
	return feature.Def{
		ID:      "F-TIM-07",
		Area:    "time",
		Name:    "Rewrite narrative",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionRewriteNarrative,
		Actions: []string{ActionRewriteNarrative},
		// app/legal/agent imports this package, so its tool and gate names are spelled out here.
		Tools: []string{
			"get_matter", "list_unbilled", "list_work_items", "propose_time_entries",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// agent.GateTimeAccept: propose_time_entries opens it on the suggested entry.
		GateKinds: []string{"timekeeping.accept"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "billing.view", "work.view", "time.record", "agent.ask"},
		Prompt:     promptFTIM07,
		PromptPath: "prompts/f-tim-07.md",
		Fixture:    "testdata/ai/f-tim-07/rewrite-narrative",
	}
}
