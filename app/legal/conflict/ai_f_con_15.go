package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// GateWall is the gate kind of spec gate G2b: an ethical wall around a matter takes effect only after a person approves it.
const GateWall = "conflict.wall"

// ActionProposeWall is the ethical-walls screen action that runs F-CON-15.
const ActionProposeWall = "ethical-walls > Propose wall"

//go:embed prompts/f-con-15.md
var promptFCON15 embed.FS

// ProposeWallDef is F-CON-15: on the ethical-walls screen, from a clearance outcome or a
// lateral-hire disclosure, the agent drafts a wall proposal with draft_document and opens gate
// conflict.wall (G2b), a tool-gap kind, with submit_for_approval. A person approves the wall.
// A screen action starts it through the screen-action route, acting for the user who asked.
func ProposeWallDef() feature.Def {
	return feature.Def{
		ID:      "F-CON-15",
		Area:    "conflicts",
		Name:    "Propose wall",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionProposeWall,
		Actions: []string{ActionProposeWall},
		Tools: []string{
			"search_contacts", "search_matters", "read_document", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateWall},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFCON15,
		PromptPath: "prompts/f-con-15.md",
		Fixture:    "testdata/ai/f-con-15/propose-wall",
	}
}
