package trust

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureAnnualReportPrep is the spec id of "annual-report-prep".
const FeatureAnnualReportPrep = "F-TRU-06"

// ActionPrepareWorksheet is the lso-annual-report screen action that runs F-TRU-06 on
// request.
const ActionPrepareWorksheet = "lso-annual-report > Prepare worksheet"

// GateRegulatoryRelease is the gate kind of spec gate G-TREL: a lawyer releases a trust
// figure to the regulator (the LSO annual report). It is a tool-gap kind: the run drafts the
// worksheet with draft_document and opens the gate with submit_for_approval.
const GateRegulatoryRelease = "trust.regulatory_release"

//go:embed prompts/f-tru-06.md
var promptFTRU06 embed.FS

// AnnualReportPrepDef is F-TRU-06: after the fiscal year ends, the agent reads the firm's
// matters, their trust balances and their bills, drafts the trust section worksheet of the
// LSO annual report with draft_document and opens gate trust.regulatory_release (G-TREL) with
// submit_for_approval. Every amount in the worksheet is a figure a read returned in the run
// (SourcedDraft); a lawyer releases it, the agent never does.
func AnnualReportPrepDef() feature.Def {
	return feature.Def{
		ID:      FeatureAnnualReportPrep,
		Area:    "trust",
		Name:    "annual-report-prep",
		Source:  feature.SourceJob,
		Trigger: "schedule (after fiscal year end)",
		Actions: []string{ActionPrepareWorksheet},
		Kind:    ai.KindExtraction,
		Tools: []string{
			"get_matter", "get_trust_balance", "get_bill", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateRegulatoryRelease},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "trust.view", "billing.view", "documents.edit", "agent.ask"},
		Prompt:     promptFTRU06,
		PromptPath: "prompts/f-tru-06.md",
		Fixture:    "testdata/ai/f-tru-06/annual-report-prep",
	}
}

// AnnualReportPrepBinding binds F-TRU-06 to its schedule. The trigger names no cadence, so
// the tick checks daily and the run writes nothing until the fiscal year has ended and no
// worksheet for it is pending. A scheduled run has no event user: it acts for the feature's
// designated lawyer from firm settings, and with none named it does not start.
func AnnualReportPrepBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(AnnualReportPrepDef())
}
