package trust

import (
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureProposeDisposition is the spec id of "Propose disposition".
const FeatureProposeDisposition = "F-TRU-10"

// GateTrustWithdrawal is G11, agent.GateTrustWithdrawal: a person other than the preparer
// authorizes money leaving the trust account. agent imports trust, so the kind is restated
// here; a test holds the two equal.
const GateTrustWithdrawal = "trust.withdrawal"

// proposeDispositionTrigger is how features.json words F-TRU-10's start: a screen action only.
const proposeDispositionTrigger = "on request: trust-balance-clearance > Propose disposition"

// ProposeDispositionDef is F-TRU-10: from the trust-balance-clearance screen, for a matter
// that still holds trust money, the agent reads the matter, its trust balance and any unpaid
// bill and proposes where the residual goes (refund to the client, or fees on a delivered
// bill) with prepare_trust_withdrawal. The withdrawal is only requested: trust.withdrawal
// (G11) stays pending until a lawyer other than the preparer authorizes it.
func ProposeDispositionDef() feature.Def {
	return feature.Def{
		ID:      FeatureProposeDisposition,
		Area:    "trust",
		Name:    "Propose disposition",
		Source:  feature.SourceScreenAction,
		Trigger: proposeDispositionTrigger,
		// The screen action is the trigger itself: "trust-balance-clearance > Propose disposition".
		Actions: []string{strings.TrimPrefix(proposeDispositionTrigger, "on request: ")},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "get_trust_balance", "get_bill",
			"prepare_trust_withdrawal", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// prepare_trust_withdrawal opens trust.withdrawal, the only kind this run may open:
		// the disposition is requested, never authorized or executed.
		GateKinds: []string{GateTrustWithdrawal},
		// The run's agent principal holds the capabilities of its tools and no other: it may
		// request a withdrawal, never authorize one.
		Grant:      []string{"agent.ask", "billing.view", "matters.view", "trust.request", "trust.view"},
		Prompt:     prompts,
		PromptPath: "prompts/f-tru-10.md",
		Fixture:    "testdata/ai/f-tru-10/propose-disposition",
	}
}
