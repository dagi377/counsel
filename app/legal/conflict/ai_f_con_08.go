package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventOrganizationClientCreated is the kernel event the contacts module publishes when an
// organization becomes a client. It starts F-CON-08.
const EventOrganizationClientCreated = "party.organization_client_created"

// Organization-identification screen actions that run F-CON-08 on request.
const (
	ActionRequestOwnershipDetails = "organization-identification > Request beneficial ownership details"
	ActionMapOwnership            = "organization-identification > Map ownership"
)

//go:embed prompts/f-con-08.md
var promptFCON08 embed.FS

// BeneficialOwnershipDef is F-CON-08: when an organization client is created, the agent maps
// its directors and beneficial owners into a draft organization record with draft_document
// and opens gate conflict.client_id (G4), a tool-gap kind, with submit_for_approval. A person
// verifies.
func BeneficialOwnershipDef() feature.Def {
	return feature.Def{
		ID:      "F-CON-08",
		Area:    "conflicts",
		Name:    "Beneficial ownership mapping",
		Source:  feature.SourceJob,
		Trigger: "event: organization client created",
		Actions: []string{ActionRequestOwnershipDetails, ActionMapOwnership},
		Tools: []string{
			"search_contacts", "search_matters", "read_document", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateClientID},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFCON08,
		PromptPath: "prompts/f-con-08.md",
		Fixture:    "testdata/ai/f-con-08/beneficial-ownership-mapping",
	}
}

// BeneficialOwnershipBinding binds F-CON-08 to the organization-client-created event. Event
// runs act for the event's user, so it needs no scheduled acting user.
func BeneficialOwnershipBinding() (trigger.Binding, error) {
	return trigger.Bind(BeneficialOwnershipDef(), "", EventOrganizationClientCreated)
}
