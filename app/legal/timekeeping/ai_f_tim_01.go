package timekeeping

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// FeatureRebuildDay is the spec id of the rebuild-day feature.
const FeatureRebuildDay = "F-TIM-01"

// Screen actions that run F-TIM-01 on request.
const (
	ActionFillGaps        = "my-time > Fill gaps"
	ActionProposeEntries  = "suggested-entries > Propose entries"
	ActionRebuildFromLogs = "activity-ledger > Rebuild day from events"
)

//go:embed prompts/f-tim-01.md
var promptFTIM01 embed.FS

// RebuildDayDef is F-TIM-01: at the end of each business day, and on request, the agent
// rebuilds the acting user's day from their matters, unbilled time and work items and
// proposes the missing time entries. propose_time_entries saves them as suggested and opens
// timekeeping.accept (G9) with the acting user as preparer; the agent never accepts an entry.
// Every work date and minute count it proposes is one a read tool returned in the run.
func RebuildDayDef() feature.Def {
	return feature.Def{
		ID:      FeatureRebuildDay,
		Area:    "time",
		Name:    "Rebuild day",
		Source:  feature.SourceJob,
		Trigger: "schedule (end of day per user, plus on demand)",
		Actions: []string{ActionFillGaps, ActionProposeEntries, ActionRebuildFromLogs},
		// app/legal/agent imports this package, so its tool and gate names are spelled out here.
		Tools: []string{
			"get_matter", "list_unbilled", "list_work_items", "propose_time_entries",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// agent.GateTimeAccept: propose_time_entries opens it on the suggested entries.
		GateKinds: []string{"timekeeping.accept"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "billing.view", "work.view", "time.record", "agent.ask"},
		Prompt:     promptFTIM01,
		PromptPath: "prompts/f-tim-01.md",
		Fixture:    "testdata/ai/f-tim-01/rebuild-day",
	}
}

// RebuildDayBinding binds F-TIM-01 to its end-of-day run. The scheduled run has no event
// user, so boot names the user it acts for; empty is trigger.ErrBadBinding.
func RebuildDayBinding(actingFor string) (trigger.Binding, error) {
	return trigger.Bind(RebuildDayDef(), actingFor)
}
