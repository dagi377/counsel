// Package billing holds the billing area's AI feature definitions. The billing modules
// themselves live in the subpackages (bill, draft, batch, payment, ...); this package only
// declares what each agent feature may read, write and open, and how it is triggered.
package billing

import (
	"embed"
	"github.com/dagi377/counsel/app/legal/billing/batch"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureBillingCycleDrafts is the spec id of "Billing cycle drafts".
const FeatureBillingCycleDrafts = "F-BIL-01"

// EventBillingCycleRequested is the kernel event a person raises to run the billing cycle
// for a matter now, outside the firm billing day (the "or manual" of F-BIL-01).
const EventBillingCycleRequested = batch.EventCycleRequested

//go:embed prompts/f-bil-01.md
var promptFBIL01 embed.FS

// BillingCycleDraftsDef is F-BIL-01: on the firm billing day (or on a manual request) the
// agent reads a matter's unbilled work and trust balance and assembles a draft pre-bill with
// draft_bill, which opens billing.prebill_review (G-B1). The bill stays a draft: a lawyer
// other than the acting user approves it, never the agent.
func BillingCycleDraftsDef() feature.Def {
	return feature.Def{
		ID:      FeatureBillingCycleDrafts,
		Area:    "billing",
		Name:    "Billing cycle drafts",
		Source:  feature.SourceJob,
		Trigger: "schedule (firm billing day per matter) or manual",
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "list_unbilled", "get_bill", "get_trust_balance",
			"draft_bill", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{agent.GatePrebillReview}, // G-B1, opened by draft_bill
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "billing.draft", "billing.view", "matters.view", "trust.view"},
		Prompt:     promptFBIL01,
		PromptPath: "prompts/f-bil-01.md",
		Fixture:    "testdata/ai/f-bil-01/billing-cycle-drafts",
	}
}

// BillingCycleDraftsBinding binds F-BIL-01 to the daily schedule, acting for the feature's
// designated lawyer (each run drafts only for matters whose billing day it is), and to the
// manual request event, which acts for the user who raised it.
func BillingCycleDraftsBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(BillingCycleDraftsDef(), EventBillingCycleRequested)
}
