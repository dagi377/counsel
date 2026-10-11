package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/deadlines"
)

// EventAbsenceRequested is the kernel event work/team-availability publishes when an absence
// is requested. With deadlines.EventAbsenceApproved it starts F-WRK-09.
const EventAbsenceRequested = "work.absence_requested"

// Screen actions that run F-WRK-09 on request.
const (
	ActionProposeCoverForAbsence = "workload > Propose cover for an absence"
	ActionProposeCover           = "team-availability > Propose cover"
	ActionWarnCoverageGap        = "team-availability > Warn on coverage gap"
)

// GateAbsence is the gate kind of spec gate G-W5: a person with work.approve accepts the
// cover an agent proposed for an absence.
const GateAbsence = "work.absence"

//go:embed prompts/f-wrk-09.md
var promptFWRK09 embed.FS

// AbsenceCoverPlannerDef is F-WRK-09: when an absence is requested or approved, and once a
// day, the agent reads the absent lawyer's matters, open work items and documents, proposes a
// cover task for each item that falls due during the absence with create_task and opens gate
// work.absence (G-W5) on it with submit_for_approval. With no one to cover, it warns of the
// gap instead. Cover counts only once a person with work.approve accepts it.
func AbsenceCoverPlannerDef() feature.Def {
	return feature.Def{
		ID:      "F-WRK-09",
		Area:    "work",
		Name:    "Absence cover planner",
		Source:  feature.SourceJob,
		Trigger: "event: absence requested or approved; schedule: daily",
		Actions: []string{ActionProposeCoverForAbsence, ActionProposeCover, ActionWarnCoverageGap},
		Tools: []string{
			"get_matter", "list_work_items", "list_documents", "read_document",
			"create_task", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateAbsence},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "work.view", "documents.view", "work.edit", "agent.ask"},
		Prompt:     promptFWRK09,
		PromptPath: "prompts/f-wrk-09.md",
		Fixture:    "testdata/ai/f-wrk-09/absence-cover-planner",
	}
}

// AbsenceCoverPlannerBinding binds F-WRK-09 to requested and approved absences and a daily
// run, all acting for the feature's designated lawyer in firm settings; with none set the
// run does not start (JOB-ACTOR).
func AbsenceCoverPlannerBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(AbsenceCoverPlannerDef(), EventAbsenceRequested, deadlines.EventAbsenceApproved)
}
