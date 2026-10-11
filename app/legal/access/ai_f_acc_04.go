package access

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// GateBreakGlassReview is the gate kind of spec gate GA7: an administrator reviews what a
// break-glass session did once it ends.
const GateBreakGlassReview = "core.break_glass_review"

// EventBreakGlassEnded is the kernel event the identity module publishes when a break-glass
// session ends, by expiry or early close. It starts F-ACC-04.
const EventBreakGlassEnded = "break_glass.ended"

//go:embed prompts/f-acc-04.md
var promptFACC04 embed.FS

// BreakGlassSummaryDef is F-ACC-04: when a break-glass session ends, the agent drafts a summary
// of what the session opened with draft_document and opens gate core.break_glass_review (GA7),
// a tool-gap kind, with submit_for_approval. An administrator other than the preparer reviews
// it.
func BreakGlassSummaryDef() feature.Def {
	return feature.Def{
		ID:      "F-ACC-04",
		Area:    "access",
		Name:    "Break-glass summary",
		Source:  feature.SourceJob,
		Trigger: "event (session ends)",
		Tools: []string{
			"search_matters", "draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateBreakGlassReview},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.edit", "agent.ask"},
		Prompt:     promptFACC04,
		PromptPath: "prompts/f-acc-04.md",
		Fixture:    "testdata/ai/f-acc-04/break-glass-summary",
	}
}

// BreakGlassSummaryBinding binds F-ACC-04 to the break-glass session end event. Event runs act
// for the event's user, so it needs no scheduled acting user.
func BreakGlassSummaryBinding() (trigger.Binding, error) {
	return trigger.Bind(BreakGlassSummaryDef(), "", EventBreakGlassEnded)
}
