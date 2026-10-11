package trust

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureRetainerCoverageScan is the spec id of "retainer-coverage-scan".
const FeatureRetainerCoverageScan = "F-TRU-07"

// Screen actions that run F-TRU-07 on request.
const (
	ActionRequestRetainerTopUp = "client-trust-ledger > Request retainer top-up"
	ActionDraftTopUpRequest    = "retainer-coverage > Draft top-up request"
)

//go:embed prompts/f-tru-07.md
var promptFTRU07 embed.FS

// RetainerCoverageScanDef is F-TRU-07: nightly, the agent compares each matter's trust
// balance with the unpaid balance of its delivered bills and, where trust does not cover
// them, drafts a retainer top-up request to the matter's client contact. draft_email with
// submit opens platform.outgoing_email (G-OUT); nothing is sent until a lawyer other than
// the preparer approves. Every amount in the email is a figure a read returned in the run
// (SourcedDraft).
func RetainerCoverageScanDef() feature.Def {
	return feature.Def{
		ID:      FeatureRetainerCoverageScan,
		Area:    "trust",
		Name:    "retainer-coverage-scan",
		Source:  feature.SourceJob,
		Trigger: "schedule (nightly)",
		Actions: []string{ActionRequestRetainerTopUp, ActionDraftTopUpRequest},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "get_trust_balance", "get_bill", "draft_email",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{"platform.outgoing_email"}, // G-OUT, opened by draft_email with submit
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "trust.view", "billing.view", "documents.edit", "agent.ask"},
		Prompt:     promptFTRU07,
		PromptPath: "prompts/f-tru-07.md",
		Fixture:    "testdata/ai/f-tru-07/retainer-coverage-scan",
	}
}

// RetainerCoverageScanBinding binds F-TRU-07 to its nightly schedule (CadenceDaily). A
// scheduled run has no event user: it acts for the feature's designated lawyer from firm
// settings, and with none named it does not start.
func RetainerCoverageScanBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(RetainerCoverageScanDef())
}
