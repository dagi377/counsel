package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// EventPortalSubmission is the kernel event the identity portal publishes when a client
// submits identity evidence. It starts F-CON-07.
const EventPortalSubmission = "identity.portal_submission"

// ActionValidateAndAuthenticate is the identity-portal screen action that runs F-CON-07 on
// request.
const ActionValidateAndAuthenticate = "identity-portal > Validate and authenticate"

//go:embed prompts/f-con-07.md
var promptFCON07 embed.FS

// IDEvidenceCheckDef is F-CON-07: when a client submits identity evidence through the portal,
// the agent extracts it into a pre-filled identity record with draft_document and opens gate
// conflict.client_id (G4), a tool-gap kind, with submit_for_approval. A person verifies.
func IDEvidenceCheckDef() feature.Def {
	return feature.Def{
		ID:      "F-CON-07",
		Area:    "conflicts",
		Name:    "ID evidence check",
		Source:  feature.SourceJob,
		Trigger: "event: portal submission",
		Actions: []string{ActionValidateAndAuthenticate},
		Kind:    ai.KindExtraction,
		Tools: []string{
			"search_contacts", "search_matters", "read_document", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateClientID},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFCON07,
		PromptPath: "prompts/f-con-07.md",
		Fixture:    "testdata/ai/f-con-07/id-evidence-check",
	}
}

// IDEvidenceCheckBinding binds F-CON-07 to the portal submission event. Event runs act for
// the event's user, so it needs no scheduled acting user.
func IDEvidenceCheckBinding() (trigger.Binding, error) {
	return trigger.Bind(IDEvidenceCheckDef(), "", EventPortalSubmission)
}
