package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventKeyDateChanged is the kernel event matter publishes when one of a matter's key dates
// (trial, closing, filing) changes. It starts F-WRK-04.
const EventKeyDateChanged = "matter.key_date_changed"

//go:embed prompts/f-wrk-04.md
var promptFWRK04 embed.FS

// DateShiftDef is F-WRK-04: when a matter's key date changes, the agent reads the matter,
// its open work and the document that moved the date, proposes the changed tasks with
// create_task and submits them for gate work.plan (G-W2). Nothing moves until a lawyer
// accepts the changes.
func DateShiftDef() feature.Def {
	return feature.Def{
		ID:      "F-WRK-04",
		Area:    "work",
		Name:    "Date-shift on key date change",
		Source:  feature.SourceJob,
		Trigger: "event: matter key date changed",
		Tools: []string{
			"get_matter", "list_work_items", "list_documents", "read_document",
			"create_task", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateWorkPlan},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "work.view", "documents.view", "work.edit", "agent.ask"},
		Prompt:     promptFWRK04,
		PromptPath: "prompts/f-wrk-04.md",
		Fixture:    "testdata/ai/f-wrk-04/date-shift-on-key-date-change",
	}
}

// DateShiftBinding binds F-WRK-04 to the key-date-changed event. Event runs act for the
// event's user, so it needs no scheduled acting user.
func DateShiftBinding() (trigger.Binding, error) {
	return trigger.Bind(DateShiftDef(), "", EventKeyDateChanged)
}
