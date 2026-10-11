package matter

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionProposeRelationship is the contact-detail screen action that starts F-MAT-18.
const ActionProposeRelationship = "contact-detail > Propose relationship"

//go:embed prompts/f-mat-18.md
var promptFMAT18 embed.FS

// ProposeRelationshipDef is F-MAT-18: on request on a contact, the agent reads a document
// such as a corporate profile and proposes the related party it names ("director of a
// company") on the matter's conflict check with propose_conflict_parties, citing the line.
// A new conflict-relevant party waits on G2 (matter.party_conflict), prepared by the acting
// user; the agent never decides it.
func ProposeRelationshipDef() feature.Def {
	return feature.Def{
		ID:      "F-MAT-18",
		Area:    "matters",
		Name:    "Propose relationship",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionProposeRelationship,
		Actions: []string{ActionProposeRelationship},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts",
			"propose_conflict_parties", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GatePartyConflict},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "conflicts.run", "contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFMAT18,
		PromptPath: "prompts/f-mat-18.md",
		Fixture:    "testdata/ai/f-mat-18/propose-relationship",
	}
}
