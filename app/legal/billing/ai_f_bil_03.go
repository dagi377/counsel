package billing

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// FeatureTrustApplication is the spec id of "Trust application proposal".
const FeatureTrustApplication = "F-BIL-03"

// EventBillDelivered is the kernel event billing publishes when a bill's delivery evidence
// sets its delivered date. It starts F-BIL-03; the run itself checks the retainer permits a
// transfer (docs/QUESTIONS.md Q-FEAT-BIL-03).
const EventBillDelivered = "bill.delivered"

// ActionProposeTransfer is the trust-to-fees screen action that runs F-BIL-03 on request.
const ActionProposeTransfer = "trust-to-fees > Propose transfer"

// GateTrustFeeWithdrawal is the gate kind of spec gate G-B4: a person authorizes a transfer
// from trust to pay a delivered bill. prepare_trust_withdrawal with a bill_id opens it
// (withdraw.RequestGateKind).
const GateTrustFeeWithdrawal = "billing.trust_fee_withdrawal"

//go:embed prompts/f-bil-03.md
var promptFBIL03 embed.FS

// TrustApplicationDef is F-BIL-03: when a bill is delivered, and on request from the
// trust-to-fees screen, the agent reads the bill and the matter's trust balance and prepares
// a transfer from trust to pay the bill. prepare_trust_withdrawal records the withdrawal as
// requested and opens billing.trust_fee_withdrawal (G-B4); no money moves until a person
// other than the acting user authorizes it (By-Law 9 s.9(1)).
func TrustApplicationDef() feature.Def {
	return feature.Def{
		ID:      FeatureTrustApplication,
		Area:    "billing",
		Name:    "Trust application proposal",
		Source:  feature.SourceJob,
		Trigger: "event: bill delivered and retainer permits",
		Actions: []string{ActionProposeTransfer},
		Tools: []string{
			"get_matter", "list_unbilled", "get_bill", "get_trust_balance",
			"prepare_trust_withdrawal", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateTrustFeeWithdrawal},
		// The run's agent principal holds the capabilities of its tools and no other: it may
		// request a withdrawal, never authorize one.
		Grant:      []string{"matters.view", "billing.view", "trust.view", "trust.request", "agent.ask"},
		Prompt:     promptFBIL03,
		PromptPath: "prompts/f-bil-03.md",
		Fixture:    "testdata/ai/f-bil-03/trust-application-proposal",
	}
}

// TrustApplicationBinding binds F-BIL-03 to the bill-delivered event. Event runs act for the
// event's user, so it needs no scheduled acting user.
func TrustApplicationBinding() (trigger.Binding, error) {
	return trigger.Bind(TrustApplicationDef(), "", EventBillDelivered)
}
