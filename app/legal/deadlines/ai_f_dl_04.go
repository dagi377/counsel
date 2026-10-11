package deadlines

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// Kernel event types that start F-DL-04. The filing and service modules publish them.
const (
	EventFilingReceiptReceived      = "filing.receipt_received"
	EventClaimIssued                = "claim.issued"
	EventAffidavitOfServiceReceived = "service.affidavit_received"
)

//go:embed prompts/f-dl-04.md
var promptFDL04 embed.FS

// ComplianceEvidenceMatcherDef is F-DL-04: when an e-filing receipt, an issued claim or an
// affidavit of service arrives, the agent matches it to the open deadline it satisfies and
// proposes marking that deadline met through cal.mark_met. A person decides.
func ComplianceEvidenceMatcherDef() feature.Def {
	return feature.Def{
		ID:      "F-DL-04",
		Area:    "deadlines",
		Name:    "Compliance evidence matcher",
		Source:  feature.SourceJob,
		Trigger: "event: e-filing receipt, issued claim or affidavit of service received",
		Kind:    ai.KindExtraction,
		Tools: []string{
			"get_matter", "read_document", "list_deadlines", "compute_deadline",
			"propose_deadline", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds:  []string{"cal.mark_met"},
		Prompt:     promptFDL04,
		PromptPath: "prompts/f-dl-04.md",
		Fixture:    "testdata/ai/f-dl-04/compliance-evidence-matcher",
	}
}

// ComplianceEvidenceMatcherBinding binds F-DL-04 to the events that start it. Event runs act
// for the event's user, so it needs no scheduled acting user.
func ComplianceEvidenceMatcherBinding() (trigger.Binding, error) {
	return trigger.Bind(ComplianceEvidenceMatcherDef(), "",
		EventFilingReceiptReceived, EventClaimIssued, EventAffidavitOfServiceReceived)
}
