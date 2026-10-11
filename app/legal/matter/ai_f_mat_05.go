package matter

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureContactDedupe is the spec id of "Contact dedupe and enrichment".
const FeatureContactDedupe = "F-MAT-05"

// GateContactMerge is G-CM: a person merges, deletes or relates the proposed contacts. It has
// no write tool of its own (a tool-gap kind), so the run drafts the proposal with
// draft_document and opens the gate with submit_for_approval.
const GateContactMerge = "matter.contact_merge"

// ContactCreatedEvents are the a2 events that start F-MAT-05 outside its daily schedule.
var ContactCreatedEvents = []string{"contact.created"}

//go:embed prompts/f-mat-05.md
var promptFMAT05 embed.FS

// ContactDedupeDef is F-MAT-05: daily and on each new contact, the agent ranks likely
// duplicate contacts, reads what the firm holds on them and drafts merge and relationship
// proposals, which it submits to G-CM (matter.contact_merge). The acting user is the
// preparer; another person decides.
func ContactDedupeDef() feature.Def {
	return feature.Def{
		ID:      FeatureContactDedupe,
		Area:    "matters",
		Name:    "Contact dedupe and enrichment",
		Source:  feature.SourceJob,
		Trigger: "schedule: daily, and on contact create",
		Actions: []string{
			"contact-list > Propose contact from email or document",
			"contact-list > Propose merge",
		},
		Kind: ai.KindEmbedding,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateContactMerge},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "contacts.view", "documents.edit", "documents.view", "matters.view"},
		Prompt:     promptFMAT05,
		PromptPath: "prompts/f-mat-05.md",
		Fixture:    "testdata/ai/f-mat-05/contact-dedupe-and-enrichment",
	}
}

// ContactDedupeBinding binds F-MAT-05 to its daily schedule and to contact.created. A
// scheduled run acts for the feature's designated lawyer in firm settings, read at each
// tick; with none named the run does not start. Event runs act for the event's user.
func ContactDedupeBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(ContactDedupeDef(), ContactCreatedEvents...)
}
