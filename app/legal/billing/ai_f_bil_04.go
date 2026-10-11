package billing

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeaturePaymentMatching is the spec id of "Payment matching".
const FeaturePaymentMatching = "F-BIL-04"

// Kernel events that start F-BIL-04: a payment provider's webhook recorded a payment, or a
// bank feed line was imported.
const (
	EventPaymentReceived   = "payment.received"
	EventBankFeedLineAdded = "bank_feed.line_imported"
)

//go:embed prompts/f-bil-04.md
var promptFBIL04 embed.FS

// PaymentMatchingDef is F-BIL-04: when a payment arrives the agent reads the matter's open
// bills and, when the match is ambiguous, proposes how to allocate the payment with
// propose_payment_allocation, which opens billing.payment_allocation (G-B3). Nothing is
// allocated until a person other than the acting user approves.
func PaymentMatchingDef() feature.Def {
	return feature.Def{
		ID:      FeaturePaymentMatching,
		Area:    "billing",
		Name:    "Payment matching",
		Source:  feature.SourceJob,
		Trigger: "event: provider webhook or bank feed line",
		Kind:    ai.KindExtraction,
		Tools: []string{
			"get_matter", "list_unbilled", "get_bill", "get_trust_balance",
			"propose_payment_allocation", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{agent.GatePaymentAllocation}, // G-B3, opened by propose_payment_allocation
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "billing.payments", "billing.view", "matters.view", "trust.view"},
		Prompt:     promptFBIL04,
		PromptPath: "prompts/f-bil-04.md",
		Fixture:    "testdata/ai/f-bil-04/payment-matching",
	}
}

// PaymentMatchingBinding binds F-BIL-04 to the payment and bank feed events, acting for the
// event's user.
func PaymentMatchingBinding() (trigger.Binding, error) {
	return trigger.Bind(PaymentMatchingDef(), "", EventPaymentReceived, EventBankFeedLineAdded)
}
