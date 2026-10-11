package conflict

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventWalledMatterAccessed is the kernel event the roles module publishes on any access to a
// walled matter. It starts F-CON-09.
const EventWalledMatterAccessed = "roles.walled_matter_accessed"

// GateWall is the gate kind of spec gate G2b: a person decides a wall breach or a wall review.
const GateWall = "conflict.wall"

//go:embed prompts/f-con-09.md
var promptFCON09 embed.FS

// WallMonitorDef is F-CON-09: on any access to a walled matter, and on each wall's review
// date, the agent checks the access against the wall and drafts a breach alert or a review
// task with draft_document, then opens gate conflict.wall (G2b), a tool-gap kind, with
// submit_for_approval. A person decides; the agent never lifts or changes a wall.
func WallMonitorDef() feature.Def {
	return feature.Def{
		ID:      "F-CON-09",
		Area:    "conflicts",
		Name:    "Wall enforcement and monitoring",
		Source:  feature.SourceJob,
		Trigger: "event: any access to a walled matter, plus schedule: review dates",
		Tools: []string{
			"search_contacts", "search_matters", "read_document", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateWall},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"contacts.view", "matters.view", "documents.view", "documents.edit", "agent.ask"},
		Prompt:     promptFCON09,
		PromptPath: "prompts/f-con-09.md",
		Fixture:    "testdata/ai/f-con-09/wall-enforcement-and-monitoring",
	}
}

// WallMonitorBinding binds F-CON-09 to walled-matter access events and to a daily check of
// wall review dates. Event runs act for the event's user; the scheduled check has none, so
// boot names the user it acts for (the conflicts officer); empty is ErrBadBinding.
func WallMonitorBinding(actingFor string) (trigger.Binding, error) {
	return trigger.Bind(WallMonitorDef(), actingFor, EventWalledMatterAccessed)
}
