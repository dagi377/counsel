package work

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventItemAgentReady is the kernel event work publishes when an item enters an Agent-ready
// status. It is one of the two events that start F-WRK-01.
const EventItemAgentReady = "work.item_agent_ready"

// EventItemAssignedToAgent is the kernel event work publishes when an item is assigned to the
// agent. It is the other event that starts F-WRK-01.
const EventItemAssignedToAgent = "work.item_assigned_to_agent"

// ActionPostResult is the work-item screen action that runs F-WRK-01 on request.
const ActionPostResult = "work-item > Post result"

// GateWorkOutput is the gate kind of spec gate G-W1: a person with work.approve accepts the
// output an agent prepared on a work item.
const GateWorkOutput = "work.output"

//go:embed prompts/f-wrk-01.md
var promptFWRK01 embed.FS

// PickUpAgentReadyDef is F-WRK-01: when a work item enters Agent-ready or is assigned to the
// agent, the agent reads the item, its matter and documents, drafts its output as a proposed
// task with create_task and opens gate work.output (G-W1) on it with submit_for_approval. The
// output counts only once a person with work.approve accepts it.
func PickUpAgentReadyDef() feature.Def {
	return feature.Def{
		ID:      "F-WRK-01",
		Area:    "work",
		Name:    "Pick up agent-ready items",
		Source:  feature.SourceJob,
		Trigger: "event: item enters Agent-ready or assigned to agent",
		Actions: []string{ActionPostResult},
		Tools: []string{
			"get_matter", "list_work_items", "list_documents", "read_document",
			"create_task", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateWorkOutput},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "work.view", "documents.view", "work.edit", "agent.ask"},
		Prompt:     promptFWRK01,
		PromptPath: "prompts/f-wrk-01.md",
		Fixture:    "testdata/ai/f-wrk-01/pick-up-agent-ready-items",
	}
}

// PickUpAgentReadyBinding binds F-WRK-01 to the agent-ready and assigned-to-agent events.
// Event runs act for the event's user, so it needs no scheduled acting user.
func PickUpAgentReadyBinding() (trigger.Binding, error) {
	return trigger.Bind(PickUpAgentReadyDef(), "", EventItemAgentReady, EventItemAssignedToAgent)
}
