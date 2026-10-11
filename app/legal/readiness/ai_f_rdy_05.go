package readiness

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// GateServiceCredit is the tool-gap gate kind of a drafted service credit note (G-SLA,
// approver billing.credit).
const GateServiceCredit = "platform.service_credit"

// serviceCreditTrigger is how features.json words F-RDY-05's start: a screen action only.
const serviceCreditTrigger = "on request: trust-center > Draft service credit note"

// ActionDraftServiceCredit is the trust-center action that runs F-RDY-05.
var ActionDraftServiceCredit = strings.TrimPrefix(serviceCreditTrigger, "on request: ")

//go:embed prompts/f-rdy-05.md
var promptFRDY05 embed.FS

// ServiceCreditDef is F-RDY-05: from trust-center the agent reads the month's uptime from the
// status page record (search_documents on the selected readiness matter) and, when it is below
// the service target, drafts the credit note with draft_document and opens gate
// platform.service_credit (G-SLA), a tool-gap kind, with submit_for_approval. The owner
// approves; the run issues no credit.
func ServiceCreditDef() feature.Def {
	return feature.Def{
		ID:      "F-RDY-05",
		Area:    "readiness",
		Name:    "Draft service credit note",
		Source:  feature.SourceScreenAction,
		Trigger: serviceCreditTrigger,
		// The screen action is the trigger itself.
		Actions: []string{ActionDraftServiceCredit},
		Tools: []string{
			agent.ToolSearchDocuments, agent.ToolDraftDocument,
			agent.ToolReportPlan, agent.ToolReportStep, agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{GateServiceCredit},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapDocumentsView, "documents.edit", agent.CapAsk},
		Prompt:     promptFRDY05,
		PromptPath: "prompts/f-rdy-05.md",
		Fixture:    "testdata/ai/f-rdy-05/draft-service-credit-note",
	}
}
