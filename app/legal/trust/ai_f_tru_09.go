package trust

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureSpotAuditPack is the spec id of "Spot-audit pack".
const FeatureSpotAuditPack = "F-TRU-09"

// ActionSpotAuditPack is the trust-audit-trail screen action that runs F-TRU-09.
const ActionSpotAuditPack = "trust-audit-trail > Spot-audit pack"

// spotAuditPackTrigger is how features.json words F-TRU-09's start: the screen action only.
const spotAuditPackTrigger = "on request: " + ActionSpotAuditPack

//go:embed prompts/f-tru-09.md
var promptFTRU09 embed.FS

// SpotAuditPackDef is F-TRU-09: on the trust-audit-trail screen the agent reads the selected
// matters, their trust balances and their bills and builds the pack an LSO examiner asks for
// with draft_document, then opens docs.outgoing (G-REL) with submit_for_approval. A lawyer
// approves before the pack is released; the agent never does. Every amount in the pack is a
// figure a read returned in the run (SourcedDraft).
func SpotAuditPackDef() feature.Def {
	return feature.Def{
		ID:      FeatureSpotAuditPack,
		Area:    "trust",
		Name:    "Spot-audit pack",
		Source:  feature.SourceScreenAction,
		Trigger: spotAuditPackTrigger,
		// The screen action is the trigger itself.
		Actions: []string{ActionSpotAuditPack},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "get_trust_balance", "get_bill", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{"docs.outgoing"}, // G-REL, opened by submit_for_approval on the draft
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "trust.view", "billing.view", "documents.edit", "agent.ask"},
		Prompt:     promptFTRU09,
		PromptPath: "prompts/f-tru-09.md",
		Fixture:    "testdata/ai/f-tru-09/spot-audit-pack",
	}
}
