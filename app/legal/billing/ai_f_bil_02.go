package billing

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureBillRender is the spec id of "Bill render and pre-flight".
const FeatureBillRender = "F-BIL-02"

// EventGateDecided is Counsel's gate.decided event; F-BIL-02 starts when a G-B1 pre-bill
// review closes.
const EventGateDecided = "gate.decided"

//go:embed prompts/f-bil-02.md
var promptFBIL02 embed.FS

// BillRenderDef is F-BIL-02: when a pre-bill review (G-B1) closes, the agent reads the bill,
// runs the pre-flight checks and drafts the rendered bill with draft_document, then submits
// it for billing.bill_approval (G-B2, a tool-gap kind opened through submit_for_approval).
// The bill stays a draft until a lawyer other than the acting user approves it.
func BillRenderDef() feature.Def {
	return feature.Def{
		ID:      FeatureBillRender,
		Area:    "billing",
		Name:    "Bill render and pre-flight",
		Source:  feature.SourceJob,
		Trigger: "event: G-B1 closed",
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "list_unbilled", "get_bill", "get_trust_balance",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{bill.GateBillApproval}, // G-B2, opened by submit_for_approval
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "billing.view", "documents.edit", "matters.view", "trust.view"},
		Prompt:     promptFBIL02,
		PromptPath: "prompts/f-bil-02.md",
		Fixture:    "testdata/ai/f-bil-02/bill-render-and-pre-flight",
	}
}

// BillRenderBinding binds F-BIL-02 to gate.decided, acting for the event's user; the run
// renders only when the decided gate is a G-B1 pre-bill review.
func BillRenderBinding() (trigger.Binding, error) {
	return trigger.Bind(BillRenderDef(), "", EventGateDecided)
}
