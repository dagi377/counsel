package timekeeping

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventTimesheetWeekSubmitted is the kernel event timekeeping publishes when a timekeeper
// submits a week of time for approval. It starts F-TIM-04.
const EventTimesheetWeekSubmitted = "timesheet.week_submitted"

// GateTimesheet is the gate kind of spec gate G9c: a reviewer other than the timekeeper
// approves a submitted week. It is a tool-gap kind: draft_document then submit_for_approval
// opens it.
const GateTimesheet = "timekeeping.timesheet"

//go:embed prompts/f-tim-04.md
var promptFTIM04 embed.FS

// TimesheetPreReviewDef is F-TIM-04: when a week is submitted, the agent reviews its time,
// drafts the flags a reviewer should look at with draft_document and opens gate
// timekeeping.timesheet (G9c) with submit_for_approval. The agent never approves time; a
// reviewer other than the acting user decides.
func TimesheetPreReviewDef() feature.Def {
	return feature.Def{
		ID:      "F-TIM-04",
		Area:    "time",
		Name:    "Timesheet pre-review",
		Source:  feature.SourceJob,
		Trigger: "event (week submitted)",
		Tools: []string{
			"get_matter", "list_unbilled", "list_work_items", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateTimesheet},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "billing.view", "work.view", "documents.edit", "agent.ask"},
		Prompt:     promptFTIM04,
		PromptPath: "prompts/f-tim-04.md",
		Fixture:    "testdata/ai/f-tim-04/timesheet-pre-review",
	}
}

// TimesheetPreReviewBinding binds F-TIM-04 to the week-submitted event. Event runs act for
// the event's user, so it needs no scheduled acting user.
func TimesheetPreReviewBinding() (trigger.Binding, error) {
	return trigger.Bind(TimesheetPreReviewDef(), "", EventTimesheetWeekSubmitted)
}
