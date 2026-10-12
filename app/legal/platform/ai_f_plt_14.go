package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventGoalAccepted is the event ask-agent emits when a lawyer accepts the step plan of a goal
// the agent proposed, with the goal's matter and the accepting lawyer as user_id. It starts
// F-PLT-14.
const EventGoalAccepted = "agent.goal_accepted"

//go:embed prompts/f-plt-14.md
var promptFPLT14 embed.FS

// GoalRunnerDef is F-PLT-14: once a lawyer accepts a goal in ask-agent, the agent reports its
// step plan, reads the goal's matter and documents and records each step with report_step. The
// work product of a regulated step is drafted on that matter with draft_document and opened as
// gate platform.skill_autonomy, a tool-gap kind, with submit_for_approval: one approval item
// per regulated step. People decide; the run files, sends and moves nothing itself.
func GoalRunnerDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-14",
		Area:    "platform",
		Name:    "Goal runner",
		Source:  feature.SourceJob,
		Trigger: "event: goal accepted in ask-agent",
		Tools: []string{
			agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument, agent.ToolDraftDocument,
			"report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{"platform.skill_autonomy"},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView, "documents.edit", agent.CapAsk},
		Prompt:     promptFPLT14,
		PromptPath: "prompts/f-plt-14.md",
		Fixture:    "testdata/ai/f-plt-14/goal-runner",
	}
}

// GoalRunnerBinding binds F-PLT-14 to an accepted goal. The run acts for the lawyer who
// accepted it (the event's user), so it has no schedule and no designated lawyer.
func GoalRunnerBinding() (trigger.Binding, error) {
	return trigger.Bind(GoalRunnerDef(), "", EventGoalAccepted)
}
