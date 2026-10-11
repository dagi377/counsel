package intake

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ActionThankReferrer is the referral-sources-report screen action that runs F-INT-21.
const ActionThankReferrer = "referral-sources-report > Draft thank-you note to referrer"

//go:embed prompts/f-int-21.md
var promptFINT21 embed.FS

// ThankReferrerDef is F-INT-21: on the referral sources report a lawyer asks the agent to
// draft a thank-you email to the selected referrer. The agent drafts it with draft_email and
// submits it, which opens gate platform.outgoing_email (G-OUT); nothing is sent until a person
// other than the acting user approves. The note names no client without consent.
func ThankReferrerDef() feature.Def {
	return feature.Def{
		ID:      "F-INT-21",
		Area:    "intake",
		Name:    "Draft thank-you note to referrer",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionThankReferrer,
		Actions: []string{ActionThankReferrer},
		Tools: []string{
			agent.ToolSearchContacts, agent.ToolSearchMatters, agent.ToolDraftEmail,
			"report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		GateKinds: []string{agent.GateOutgoingEmail},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{agent.CapContactsView, agent.CapMattersView, "documents.edit", agent.CapAsk},
		Prompt:     promptFINT21,
		PromptPath: "prompts/f-int-21.md",
		Fixture:    "testdata/ai/f-int-21/draft-thank-you-note-to-referrer",
	}
}
