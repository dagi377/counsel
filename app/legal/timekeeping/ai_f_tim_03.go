package timekeeping

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventBillDraftRequested is the kernel event billing publishes when a user asks for a draft
// bill on a matter. It starts F-TIM-03 before the bill is drafted.
const EventBillDraftRequested = "bill.draft_requested"

// ActionPreBillCheck is the matter-wip screen action that runs F-TIM-03 on request.
const ActionPreBillCheck = "matter-wip > Pre-bill check"

// ActionProposeReduction is the write-downs screen action that runs F-TIM-03 on request.
const ActionProposeReduction = "write-downs > Propose reduction"

// GateWriteDown is the gate kind of spec gate G9d: a person approves a write-down before it
// changes a bill line. It is a tool-gap kind: draft_document then submit_for_approval opens
// it.
const GateWriteDown = "timekeeping.writedown"

//go:embed prompts/f-tim-03.md
var promptFTIM03 embed.FS

// PreBillCheckDef is F-TIM-03: before a bill is drafted, and monthly, the agent reviews a
// matter's unbilled time, drafts write-down proposals with draft_document and opens gate
// timekeeping.writedown (G9d) with submit_for_approval. No amount changes until a person
// other than the acting user approves.
func PreBillCheckDef() feature.Def {
	return feature.Def{
		ID:      "F-TIM-03",
		Area:    "time",
		Name:    "Pre-bill check",
		Source:  feature.SourceJob,
		Trigger: "event (bill draft requested) or schedule (monthly)",
		Actions: []string{ActionPreBillCheck, ActionProposeReduction},
		Tools: []string{
			"get_matter", "list_unbilled", "list_work_items", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateWriteDown},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "billing.view", "work.view", "documents.edit", "agent.ask"},
		Prompt:     promptFTIM03,
		PromptPath: "prompts/f-tim-03.md",
		Fixture:    "testdata/ai/f-tim-03/pre-bill-check",
	}
}

// PreBillCheckBinding binds F-TIM-03 to the bill-draft-requested event and the monthly
// schedule. Event runs act for the event's user; the monthly run acts for the feature's
// designated lawyer in firm settings (trigger.BindDesignated), never the rule admin.
func PreBillCheckBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(PreBillCheckDef(), EventBillDraftRequested)
}
