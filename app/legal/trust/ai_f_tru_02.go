package trust

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureWithdrawalDrafter is the spec id of "withdrawal-drafter".
const FeatureWithdrawalDrafter = "F-TRU-02"

// ActionDraftWithdrawal is the withdrawal-request screen action that runs F-TRU-02 on request
// (a client's direction to pay out trust money is started this way).
const ActionDraftWithdrawal = "withdrawal-request > Draft request"

// Events that start F-TRU-02 (spec/eng/a2-interfaces.json events).
const (
	// EventBillDelivered: a bill reached the payer; with trust applied, its fees may now leave
	// trust up to its unpaid balance.
	EventBillDelivered = "bill.delivered"
	// EventMatterClosed: the matter closed; any money left in trust goes back to the client.
	EventMatterClosed = "matter.closed"
)

// GateWithdrawal is the gate kind a prepared trust withdrawal opens (spec gate G11): the
// same kind as withdraw.GateKind and agent.GateTrustWithdrawal. Package trust can import
// neither (both reach trust), so the test pins the three equal.
const GateWithdrawal = "trust.withdrawal"

// capTrustRequest is the capability prepare_trust_withdrawal needs (agent.CapTrustRequest).
const capTrustRequest = "trust.request"

//go:embed prompts/f-tru-02.md
var promptFTRU02 embed.FS

// WithdrawalDrafterDef is F-TRU-02: when a bill with trust applied is delivered, a matter
// closes or a client directs a payment, the agent reads the matter, its trust balance and the
// bill, plans, and prepares one trust withdrawal with prepare_trust_withdrawal. The tool only
// requests it: gate trust.withdrawal (G11) opens with the acting user as preparer, and a
// lawyer other than the preparer authorizes. The agent never approves, authorizes or
// executes; the amount is a figure a read returned in the run, the payee a matter contact or,
// for fees on a delivered bill only, the firm's general account.
func WithdrawalDrafterDef() feature.Def {
	return feature.Def{
		ID:      FeatureWithdrawalDrafter,
		Area:    "trust",
		Name:    "withdrawal-drafter",
		Source:  feature.SourceJob,
		Trigger: "event (bill delivered with trust applied; closing; client direction)",
		Actions: []string{ActionDraftWithdrawal},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "get_trust_balance", "get_bill", "prepare_trust_withdrawal",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// The run may open trust.withdrawal and no other gate kind.
		GateKinds: []string{GateWithdrawal},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "billing.view", "matters.view", capTrustRequest, "trust.view"},
		Prompt:     promptFTRU02,
		PromptPath: "prompts/f-tru-02.md",
		Fixture:    "testdata/ai/f-tru-02/withdrawal-drafter",
	}
}

// WithdrawalDrafterBinding binds F-TRU-02 to a delivered bill and a closed matter. Event runs
// act for the event's user. A client's direction has no event: the withdrawal-request screen
// action starts it.
func WithdrawalDrafterBinding() (trigger.Binding, error) {
	return trigger.Bind(WithdrawalDrafterDef(), "", EventBillDelivered, EventMatterClosed)
}
