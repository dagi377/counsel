package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventMatterOpened is the kernel event matter publishes when a matter is opened. It is one
// of the two events that start F-WRK-02.
const EventMatterOpened = "matter.opened"

// EventTemplateApplied is the kernel event matter publishes when a matter template is
// applied (gate matter.apply_template). It is the other event that starts F-WRK-02.
const EventTemplateApplied = "matter.template_applied"

// GateWorkPlan is the gate kind of spec gate G-W2: a person with work.approve accepts the
// work plan an agent proposed for a matter.
const GateWorkPlan = "work.plan"

//go:embed prompts/f-wrk-02.md
var promptFWRK02 embed.FS

// ProposeMatterPlanDef is F-WRK-02: when a matter is opened or a template applied, the agent
// reads the matter, its open work and its documents, proposes the plan's tasks with
// create_task and submits the plan for gate work.plan (G-W2). No task becomes work until a
// person with work.approve accepts the plan.
func ProposeMatterPlanDef() feature.Def {
	return feature.Def{
		ID:      "F-WRK-02",
		Area:    "work",
		Name:    "Propose matter plan",
		Source:  feature.SourceJob,
		Trigger: "event: matter opened or template applied",
		Tools: []string{
			"get_matter", "list_work_items", "list_documents", "read_document",
			"create_task", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateWorkPlan},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "work.view", "documents.view", "work.edit", "agent.ask"},
		Prompt:     promptFWRK02,
		PromptPath: "prompts/f-wrk-02.md",
		Fixture:    "testdata/ai/f-wrk-02/propose-matter-plan",
	}
}

// ProposeMatterPlanBinding binds F-WRK-02 to the matter-opened and template-applied events.
// Event runs act for the event's user, so it needs no scheduled acting user.
func ProposeMatterPlanBinding() (trigger.Binding, error) {
	return trigger.Bind(ProposeMatterPlanDef(), "", EventMatterOpened, EventTemplateApplied)
}
