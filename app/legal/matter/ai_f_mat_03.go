package matter

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureStageAdvance is the spec id of "Stage advance watcher".
const FeatureStageAdvance = "F-MAT-03"

// GateStageMove is G-ST: a person moves the matter to the proposed stage. It has no write
// tool of its own (a tool-gap kind), so the run drafts the proposal with draft_document and
// opens the gate with submit_for_approval.
const GateStageMove = "matter.stage_move"

// StageEvidenceEvents are the kernel events that are evidence a matter may be ready for its
// next stage: a document signed off (document.released) or filed (document.uploaded), a
// payment received, a deadline passed (deadline.met). Each starts F-MAT-03.
var StageEvidenceEvents = []string{"document.released", "document.uploaded", "payment.received", "deadline.met"}

//go:embed prompts/f-mat-03.md
var promptFMAT03 embed.FS

// StageAdvanceDef is F-MAT-03: on evidence that a matter's stage work is done, the agent
// reads the matter and its documents and drafts a stage move proposal, which it submits to
// G-ST (matter.stage_move). The acting user is the preparer; another person decides.
func StageAdvanceDef() feature.Def {
	return feature.Def{
		ID:      FeatureStageAdvance,
		Area:    "matters",
		Name:    "Stage advance watcher",
		Source:  feature.SourceJob,
		Trigger: "event: evidence event (signed, filed, paid, deadline passed)",
		Actions: []string{"matter-stage-board > Propose stage advance"},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateStageMove},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "contacts.view", "documents.edit", "documents.view", "matters.view"},
		Prompt:     promptFMAT03,
		PromptPath: "prompts/f-mat-03.md",
		Fixture:    "testdata/ai/f-mat-03/stage-advance-watcher",
	}
}

// StageAdvanceBinding binds F-MAT-03 to the evidence events. Event runs act for the event's
// user. The on-request start is the Def's screen action, served by trigger.Actions.
func StageAdvanceBinding() (trigger.Binding, error) {
	return trigger.Bind(StageAdvanceDef(), "", StageEvidenceEvents...)
}
