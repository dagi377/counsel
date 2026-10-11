package timekeeping

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// EventEngagementLetterSigned is the kernel event the documents module publishes when a client
// signs a matter's engagement letter. It starts F-TIM-05.
const EventEngagementLetterSigned = "engagement_letter.signed"

// ActionExtractTerms is the matter-fee-arrangement screen action that runs F-TIM-05 on
// request.
const ActionExtractTerms = "matter-fee-arrangement > Extract terms from engagement letter"

// GateFeeArrangement is the gate kind of spec gate G9e: a lawyer approves a matter's fee
// arrangement before it prices any time. It is a tool-gap kind: the run drafts the
// arrangement with draft_document and opens the gate with submit_for_approval.
const GateFeeArrangement = "timekeeping.fee_arrangement"

//go:embed prompts/f-tim-05.md
var promptFTIM05 embed.FS

// EngagementTermsDef is F-TIM-05: when an engagement letter is signed, the agent reads the
// matter, its unbilled time and its tasks, extracts the fee terms into a draft FeeArrangement
// with draft_document and opens gate timekeeping.fee_arrangement (G9e) with
// submit_for_approval. A lawyer approves the arrangement; the agent never does.
func EngagementTermsDef() feature.Def {
	return feature.Def{
		ID:      "F-TIM-05",
		Area:    "time",
		Name:    "Engagement terms extract",
		Source:  feature.SourceJob,
		Trigger: "event (engagement letter signed)",
		Actions: []string{ActionExtractTerms},
		Kind:    ai.KindExtraction,
		Tools: []string{
			"get_matter", "list_unbilled", "list_work_items", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateFeeArrangement},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "billing.view", "work.view", "documents.edit", "agent.ask"},
		Prompt:     promptFTIM05,
		PromptPath: "prompts/f-tim-05.md",
		Fixture:    "testdata/ai/f-tim-05/engagement-terms-extract",
	}
}

// EngagementTermsBinding binds F-TIM-05 to the signed engagement letter. Event runs act for
// the event's user.
func EngagementTermsBinding() (trigger.Binding, error) {
	return trigger.Bind(EngagementTermsDef(), "", EventEngagementLetterSigned)
}
