package matter

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeaturePartyExtraction is the spec id of the matters "Party extraction".
const FeaturePartyExtraction = "F-MAT-04"

// GatePartyConflict is G2 on a matter: a party joins the matter in its role only once a
// lawyer clears the conflict check propose_conflict_parties runs on it.
const GatePartyConflict = "matter.party_conflict"

// PartyDocumentEvents are the a2 events of a document filed on a matter (a new pleading,
// correspondence or closing document): document.uploaded. Each starts F-MAT-04.
var PartyDocumentEvents = []string{"document.uploaded"}

//go:embed prompts/f-mat-04.md
var promptFMAT04 embed.FS

// PartyExtractionDef is F-MAT-04: the agent reads a new pleading, correspondence or closing
// document on a matter and proposes the parties it names, with their roles, for the
// matter's conflict check. The check waits on G2 (matter.party_conflict), prepared by the
// acting user; a lawyer other than them clears it.
func PartyExtractionDef() feature.Def {
	return feature.Def{
		ID:      FeaturePartyExtraction,
		Area:    "matters",
		Name:    "Party extraction",
		Source:  feature.SourceJob,
		Trigger: "event: new pleading, correspondence or closing document",
		Actions: []string{"matter-parties > Propose party from document or email"},
		Kind:    ai.KindExtraction,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts",
			"propose_conflict_parties", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GatePartyConflict},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "conflicts.run", "contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFMAT04,
		PromptPath: "prompts/f-mat-04.md",
		Fixture:    "testdata/ai/f-mat-04/party-extraction",
	}
}

// PartyExtractionBinding binds F-MAT-04 to a document filed on a matter. Event runs act for
// the event's user. The on-request start is the Def's screen action, served by
// trigger.Actions.
func PartyExtractionBinding() (trigger.Binding, error) {
	return trigger.Bind(PartyExtractionDef(), "", PartyDocumentEvents...)
}
