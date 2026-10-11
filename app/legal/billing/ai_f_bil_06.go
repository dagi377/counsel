package billing

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureAgingCollections is the spec id of "Aging and collections".
const FeatureAgingCollections = "F-BIL-06"

// GateCollectionsMessage is the gate kind of spec gate G-B7: a lawyer approves a payment
// reminder before it is sent to the client.
const GateCollectionsMessage = "billing.collections_message"

//go:embed prompts/f-bil-06.md
var promptFBIL06 embed.FS

// AgingCollectionsDef is F-BIL-06: every business day the agent reads overdue bills and
// drafts a payment reminder with draft_email (a draft only), then opens
// billing.collections_message (G-B7) on it with submit_for_approval. It prepares only:
// nothing is sent and no payment, write-off or trust transfer is made until a person other
// than the preparer decides.
func AgingCollectionsDef() feature.Def {
	return feature.Def{
		ID:      FeatureAgingCollections,
		Area:    "billing",
		Name:    "Aging and collections",
		Source:  feature.SourceJob,
		Trigger: "schedule daily",
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "list_unbilled", "get_bill", "get_trust_balance",
			"draft_email", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateCollectionsMessage}, // G-B7, opened by submit_for_approval
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "billing.view", "documents.edit", "matters.view", "trust.view"},
		Prompt:     promptFBIL06,
		PromptPath: "prompts/f-bil-06.md",
		Fixture:    "testdata/ai/f-bil-06/aging-and-collections",
	}
}

// AgingCollectionsBinding binds F-BIL-06 to its daily schedule. A scheduled run has no event
// user, so it acts for the feature's designated lawyer from firm settings.
func AgingCollectionsBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(AgingCollectionsDef())
}
