package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionPrepareTrustRefund is the matter-closing screen action that runs F-PLT-21.
const ActionPrepareTrustRefund = "matter-closing > Prepare trust refund / transfer"

//go:embed prompts/f-plt-21.md
var promptFPLT21 embed.FS

// PrepareTrustRefundDef is F-PLT-21: on the matter-closing screen a lawyer asks the agent to
// prepare the refund, or the transfer, of the money left in trust for the selected matter.
// The agent reads the trust ledger statement and the closing documents and calls
// prepare_trust_withdrawal, which records the withdrawal as requested and opens gate
// trust.withdrawal (G11) through the scoped opener. No money moves until a lawyer other than
// the acting user authorizes it; the agent never does. An overdraft or an unreadable balance
// is reported, never drafted around. A screen action starts it, acting for the user who
// asked, so it binds to no event or schedule.
func PrepareTrustRefundDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-21",
		Area:    "platform",
		Name:    "Prepare trust refund / transfer",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionPrepareTrustRefund,
		Actions: []string{ActionPrepareTrustRefund},
		Tools: []string{
			agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument, agent.ToolPrepareTrustWithdrawal,
			"report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{agent.GateTrustWithdrawal},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView, agent.CapTrustRequest, agent.CapAsk},
		Prompt:     promptFPLT21,
		PromptPath: "prompts/f-plt-21.md",
		Fixture:    "testdata/ai/f-plt-21/prepare-trust-refund-transfer",
	}
}
