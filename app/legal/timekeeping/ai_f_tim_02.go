package timekeeping

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// EventDocumentUploaded is the kernel event the documents module publishes when a file is
// uploaded to a matter. A receipt upload starts F-TIM-02.
const EventDocumentUploaded = "document.uploaded"

// EventEmailFiled is the kernel event comms publishes when an email is filed to a matter. A
// receipt email starts F-TIM-02.
const EventEmailFiled = "email.filed"

// ActionExtractFromReceipt is the expenses screen action that runs F-TIM-02 on request.
const ActionExtractFromReceipt = "expenses > Extract from receipt"

// GateDisbursement is the gate kind of spec gate G9b: a person approves a disbursement
// before it is recorded against the matter. It is a tool-gap kind: draft_document then
// submit_for_approval opens it.
const GateDisbursement = "timekeeping.disbursement"

//go:embed prompts/f-tim-02.md
var promptFTIM02 embed.FS

// ReceiptCaptureDef is F-TIM-02: when a receipt is emailed or uploaded to a matter, the agent
// extracts it into a draft expense with draft_document and opens gate
// timekeeping.disbursement (G9b) with submit_for_approval. Nothing is recorded until a person
// other than the acting user approves.
func ReceiptCaptureDef() feature.Def {
	return feature.Def{
		ID:      "F-TIM-02",
		Area:    "time",
		Name:    "Receipt capture",
		Source:  feature.SourceJob,
		Trigger: "event (receipt email or upload)",
		Actions: []string{ActionExtractFromReceipt},
		Kind:    ai.KindExtraction,
		Tools: []string{
			"get_matter", "list_unbilled", "list_work_items", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateDisbursement},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "billing.view", "work.view", "documents.edit", "agent.ask"},
		Prompt:     promptFTIM02,
		PromptPath: "prompts/f-tim-02.md",
		Fixture:    "testdata/ai/f-tim-02/receipt-capture",
	}
}

// ReceiptCaptureBinding binds F-TIM-02 to a document uploaded or an email filed to a matter.
// Event runs act for the event's user, so it needs no scheduled acting user. The prompt has
// the agent draft only when the file is a receipt.
func ReceiptCaptureBinding() (trigger.Binding, error) {
	return trigger.Bind(ReceiptCaptureDef(), "", EventDocumentUploaded, EventEmailFiled)
}
