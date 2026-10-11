package timekeeping

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
)

// EventRateChangeDrafted is the kernel event the rates module publishes when a rate change is
// drafted, before it takes effect. It starts F-TIM-06.
const EventRateChangeDrafted = "rate_change.drafted"

//go:embed prompts/f-tim-06.md
var promptFTIM06 embed.FS

// RateChangeImpactDef is F-TIM-06: when a rate change is drafted, the agent reads each
// affected matter, its unbilled time and its open tasks, drafts a client notice of the change
// and its impact with draft_document and opens gate docs.outgoing (G8) with
// submit_for_approval. Nothing leaves the firm until a lawyer approves the letter.
func RateChangeImpactDef() feature.Def {
	return feature.Def{
		ID:      "F-TIM-06",
		Area:    "time",
		Name:    "Rate change impact",
		Source:  feature.SourceJob,
		Trigger: "event (rate change drafted)",
		Tools: []string{
			"get_matter", "list_unbilled", "list_work_items", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{legaldocs.GateOutgoing},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "billing.view", "work.view", "documents.edit", "agent.ask"},
		Prompt:     promptFTIM06,
		PromptPath: "prompts/f-tim-06.md",
		Fixture:    "testdata/ai/f-tim-06/rate-change-impact",
	}
}

// RateChangeImpactBinding binds F-TIM-06 to the drafted rate change. Event runs act for the
// event's user.
func RateChangeImpactBinding() (trigger.Binding, error) {
	return trigger.Bind(RateChangeImpactDef(), "", EventRateChangeDrafted)
}
