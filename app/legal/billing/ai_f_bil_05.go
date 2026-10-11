package billing

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureEvergreenWatch is the spec id of "Evergreen watch".
const FeatureEvergreenWatch = "F-BIL-05"

// ActionDraftRetainerRequest is the retainer-requests screen action that runs F-BIL-05 on
// request.
const ActionDraftRetainerRequest = "retainer-requests > Draft request"

//go:embed prompts/f-bil-05.md
var promptFBIL05 embed.FS

// EvergreenWatchDef is F-BIL-05: every business day the agent reads each matter's trust
// balance, unbilled work and open bills and, where an evergreen retainer has run low, drafts a
// retainer request to the client with draft_email and submit, which opens
// platform.outgoing_email (G-OUT). It prepares only: nothing is sent and no trust money moves
// until a person other than the preparer approves.
func EvergreenWatchDef() feature.Def {
	return feature.Def{
		ID:      FeatureEvergreenWatch,
		Area:    "billing",
		Name:    "Evergreen watch",
		Source:  feature.SourceJob,
		Trigger: "schedule daily",
		Actions: []string{ActionDraftRetainerRequest},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "list_unbilled", "get_bill", "get_trust_balance",
			"draft_email", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{agent.GateOutgoingEmail}, // G-OUT, opened by draft_email with submit
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "billing.view", "documents.edit", "matters.view", "trust.view"},
		Prompt:     promptFBIL05,
		PromptPath: "prompts/f-bil-05.md",
		Fixture:    "testdata/ai/f-bil-05/evergreen-watch",
	}
}

// EvergreenWatchBinding binds F-BIL-05 to its daily schedule. A scheduled run has no event
// user, so it acts for the feature's designated lawyer from firm settings.
func EvergreenWatchBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(EvergreenWatchDef())
}
