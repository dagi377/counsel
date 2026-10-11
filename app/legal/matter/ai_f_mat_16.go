package matter

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// GatePartyConflict is G2 (matters): a party added to a matter that was not in its original
// conflict check re-opens the conflict decision. A lawyer other than the preparer decides it.
const GatePartyConflict = "matter.party_conflict"

// ActionRerunConflict is the matter-open-wizard screen action that starts F-MAT-16.
const ActionRerunConflict = "matter-open-wizard > Re-run conflict on new parties"

//go:embed prompts/f-mat-16.md
var promptFMAT16 embed.FS

// RerunConflictDef is F-MAT-16: on request in the matter-open wizard, the agent finds the
// parties added since the original conflict check and proposes them on that check with
// propose_conflict_parties. The re-run check waits on G2 (matter.party_conflict), prepared by
// the acting user; the agent never decides it.
func RerunConflictDef() feature.Def {
	return feature.Def{
		ID:      "F-MAT-16",
		Area:    "matters",
		Name:    "Re-run conflict on new parties",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionRerunConflict,
		Actions: []string{ActionRerunConflict},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts",
			"propose_conflict_parties", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GatePartyConflict},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "conflicts.run", "contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFMAT16,
		PromptPath: "prompts/f-mat-16.md",
		Fixture:    "testdata/ai/f-mat-16/re-run-conflict-on-new-parties",
	}
}
