package deadlines

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// Kernel events the calendar publishes when a hearing moves or is cancelled. They start
// F-DL-08.
const (
	EventCalendarEventMoved     = "calendar.event_moved"
	EventCalendarEventCancelled = "calendar.event_cancelled"
)

//go:embed prompts/f-dl-08.md
var promptFDL08 embed.FS

// AdjournmentWatcherDef is F-DL-08: when a hearing moves or is cancelled, the agent
// recomputes the deadlines that ran from it and sends the recompute proposal to the
// extraction review under gate cal.change. Every proposed date comes from compute_deadline
// or a cited document in the run.
func AdjournmentWatcherDef() feature.Def {
	return feature.Def{
		ID:      "F-DL-08",
		Area:    "deadlines",
		Name:    "Adjournment watcher",
		Source:  feature.SourceJob,
		Trigger: "event: hearing event moved or cancelled",
		Kind:    ai.KindExtraction,
		Tools: []string{
			"get_matter", "read_document", "list_deadlines", "compute_deadline",
			"propose_deadline", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{"cal.change"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.view", "deadlines.view", "deadlines.edit", "agent.ask"},
		Prompt:     promptFDL08,
		PromptPath: "prompts/f-dl-08.md",
		Fixture:    "testdata/ai/f-dl-08/adjournment-watcher",
	}
}

// AdjournmentWatcherBinding binds F-DL-08 to the hearing moved and cancelled events. Event
// runs act for the event's user, so it needs no scheduled acting user.
func AdjournmentWatcherBinding() (trigger.Binding, error) {
	return trigger.Bind(AdjournmentWatcherDef(), "", EventCalendarEventMoved, EventCalendarEventCancelled)
}
