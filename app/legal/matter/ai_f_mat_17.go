package matter

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// GateWorkPlan is G-W2: a proposed matter task plan waits on it, and the agent may start
// only the template items until a person accepts the plan.
const GateWorkPlan = "work.plan"

// ActionOpeningPlan is the matter-open-wizard screen action that starts F-MAT-17.
const ActionOpeningPlan = "matter-open-wizard > Create folders, task plan and calendar placeholders"

//go:embed prompts/f-mat-17.md
var promptFMAT17 embed.FS

// OpeningPlanDef is F-MAT-17: on request after a matter opens, the agent proposes the
// opening task plan (folder set-up, first tasks and calendar placeholders) with create_task.
// Each task starts proposed and the plan waits on G-W2 (work.plan), prepared by the acting
// user. A date is a placeholder only: deadlines stay unconfirmed until G7.
func OpeningPlanDef() feature.Def {
	return feature.Def{
		ID:      "F-MAT-17",
		Area:    "matters",
		Name:    "Create folders, task plan and calendar placeholders",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionOpeningPlan,
		Actions: []string{ActionOpeningPlan},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts",
			"create_task", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateWorkPlan},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "contacts.view", "documents.view", "matters.view", "work.edit"},
		Prompt:     promptFMAT17,
		PromptPath: "prompts/f-mat-17.md",
		Fixture:    "testdata/ai/f-mat-17/create-folders-task-plan-and-calendar-placeholders",
	}
}
