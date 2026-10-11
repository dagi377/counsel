// Package deadlines holds Counsel's deadline AI features (spec/ai/features.json area
// "deadlines"). Each feature is a feature.Def that app/legal/boot adds to its feature.Set
// explicitly, plus the trigger binding that starts it. Nothing registers from init().
package deadlines

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// Kernel event types that start F-DL-03. The deadline and rule-pack modules publish them.
const (
	EventTriggerDateChanged = "deadline.trigger_date_changed"
	EventRulePackPublished  = "rulepack.published"
	EventHolidayAdded       = "holiday.added"
)

//go:embed prompts/f-dl-03.md
var promptFDL03 embed.FS

// RecomputeDependantsDef is F-DL-03: when a trigger date, rule pack or holiday changes, the
// agent recomputes the dependent deadlines and proposes the changed dates for cal.confirm.
// The old dates stay active until a person confirms.
func RecomputeDependantsDef() feature.Def {
	return feature.Def{
		ID:      "F-DL-03",
		Area:    "deadlines",
		Name:    "Recompute dependants",
		Source:  feature.SourceJob,
		Trigger: "event: trigger date changed, rule pack published, or holiday added",
		Tools: []string{
			"get_matter", "read_document", "list_deadlines", "compute_deadline",
			"propose_deadline", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds:  []string{"cal.confirm"},
		Prompt:     promptFDL03,
		PromptPath: "prompts/f-dl-03.md",
		Fixture:    "testdata/ai/f-dl-03/recompute-dependants",
	}
}

// RecomputeDependantsBinding binds F-DL-03 to the events that start it. Event runs act for the
// event's user, so it needs no scheduled acting user.
func RecomputeDependantsBinding() (trigger.Binding, error) {
	return trigger.Bind(RecomputeDependantsDef(), "", EventTriggerDateChanged, EventRulePackPublished, EventHolidayAdded)
}
