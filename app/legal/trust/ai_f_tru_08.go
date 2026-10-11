package trust

import (
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureDraftTransfer is the spec id of "Draft transfer".
const FeatureDraftTransfer = "F-TRU-08"

// GateTrustTransfer is G11t, withdraw.TransferGateKind: a person other than the preparer
// authorizes a ledger-to-ledger transfer. withdraw imports trust, so the kind is restated
// here; a test holds the two equal.
const GateTrustTransfer = "trust.transfer"

// draftTransferTrigger is how features.json words F-TRU-08's start: a screen action only.
const draftTransferTrigger = "on request: trust-transfers > Draft transfer"

// DraftTransferDef is F-TRU-08: from the trust-transfers screen (a matter split, or money
// moving to an interest-bearing account) the agent reads the matters, their trust balances
// and any bill, and prepares the transfer with prepare_trust_transfer. The run may open
// trust.transfer (G11t) only; a lawyer other than the preparer authorizes it. The agent never
// authorizes, posts or executes a transfer.
func DraftTransferDef() feature.Def {
	return feature.Def{
		ID:      FeatureDraftTransfer,
		Area:    "trust",
		Name:    "Draft transfer",
		Source:  feature.SourceScreenAction,
		Trigger: draftTransferTrigger,
		// The screen action is the trigger itself: "trust-transfers > Draft transfer".
		Actions: []string{strings.TrimPrefix(draftTransferTrigger, "on request: ")},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "get_trust_balance", "get_bill",
			"prepare_trust_transfer", "prepare_trust_withdrawal", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// The run scope holds trust.transfer only: prepare_trust_transfer opens it.
		// prepare_trust_withdrawal opens trust.withdrawal, so in this run it is refused before
		// the trust module records anything: a transfer is never filed as a withdrawal
		// (docs/QUESTIONS.md Q-FEAT-TRU-08, Q-TRU-FIX).
		GateKinds: []string{GateTrustTransfer},
		// The run's agent principal holds the capabilities of its tools and no other: it may
		// request trust money movement, never authorize it.
		Grant:      []string{"agent.ask", "billing.view", "matters.view", "trust.request", "trust.view"},
		Prompt:     prompts,
		PromptPath: "prompts/f-tru-08.md",
		Fixture:    "testdata/ai/f-tru-08/draft-transfer",
	}
}
