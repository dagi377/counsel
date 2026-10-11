package deadlines

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventCalendarEventEnded is the kernel event the calendar publishes when an event's end time
// passes. It starts F-DL-07.
const EventCalendarEventEnded = "calendar.event_ended"

// ActionDraftFollowUp is the event-detail screen action that runs F-DL-07 on request.
const ActionDraftFollowUp = "event-detail > Draft follow-up note after the event"

//go:embed prompts/f-dl-07.md
var promptFDL07 embed.FS

// PostEventFollowUpDef is F-DL-07: after a calendar event ends, the agent drafts a follow-up
// file note for the event's matter with a suggested time entry. draft_note opens the
// matter.agent_note gate (G-NT); the note is on the file only once a person approves it.
func PostEventFollowUpDef() feature.Def {
	return feature.Def{
		ID:      "F-DL-07",
		Area:    "deadlines",
		Name:    "Post-event follow-up",
		Source:  feature.SourceJob,
		Trigger: "event: event end time passes",
		Actions: []string{ActionDraftFollowUp},
		Tools: []string{
			"get_matter", "read_document", "list_deadlines", "compute_deadline",
			"draft_note", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{"matter.agent_note"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.view", "deadlines.view", "matters.edit", "agent.ask"},
		Prompt:     promptFDL07,
		PromptPath: "prompts/f-dl-07.md",
		Fixture:    "testdata/ai/f-dl-07/post-event-follow-up",
	}
}

// PostEventFollowUpBinding binds F-DL-07 to the calendar's event-ended event. Event runs act
// for the event's user, so it needs no scheduled acting user.
func PostEventFollowUpBinding() (trigger.Binding, error) {
	return trigger.Bind(PostEventFollowUpDef(), "", EventCalendarEventEnded)
}
