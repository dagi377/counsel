package deadlines

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventAbsenceApproved is the kernel event work/team-availability publishes when an absence
// is approved. It starts F-DL-09.
const EventAbsenceApproved = "work.absence_approved"

//go:embed prompts/f-dl-09.md
var promptFDL09 embed.FS

// AbsenceCoverageCheckDef is F-DL-09: when an absence is approved, and once a business day,
// the agent finds the deadlines that fall due while their lawyer is away and proposes a
// cover task for the work.absence gate, which the responsible or supervising lawyer accepts.
// With no delegate it warns instead.
func AbsenceCoverageCheckDef() feature.Def {
	return feature.Def{
		ID:      "F-DL-09",
		Area:    "deadlines",
		Name:    "Absence coverage check",
		Source:  feature.SourceJob,
		Trigger: "event: absence approved in work/team-availability; schedule: daily",
		Tools: []string{
			"get_matter", "read_document", "list_deadlines", "compute_deadline",
			"create_task", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{"work.absence"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.view", "deadlines.view", "work.edit", "agent.ask"},
		Prompt:     promptFDL09,
		PromptPath: "prompts/f-dl-09.md",
		Fixture:    "testdata/ai/f-dl-09/absence-coverage-check",
	}
}

// AbsenceCoverageCheckBinding binds F-DL-09 to approved absences and a daily run. The daily
// run has no event user, so boot names the user it acts for; empty is ErrBadBinding.
func AbsenceCoverageCheckBinding(actingFor string) (trigger.Binding, error) {
	return trigger.Bind(AbsenceCoverageCheckDef(), actingFor, EventAbsenceApproved)
}
