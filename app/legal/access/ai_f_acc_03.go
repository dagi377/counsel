package access

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// GateReviewClose is the gate kind of spec gate GA8: an administrator closes an access review
// and its decisions apply only in the gate's effect.
const GateReviewClose = "core.review_close"

// ActionPreFillReview is the access-review screen action that runs F-ACC-03 on request.
const ActionPreFillReview = "access-review > Pre-fill"

//go:embed prompts/f-acc-03.md
var promptFACC03 embed.FS

// AccessReviewPreFillDef is F-ACC-03: each quarter, or on request from the access-review
// screen, the agent drafts the review's keep and revoke decisions with draft_document and
// opens gate core.review_close (GA8), a tool-gap kind, with submit_for_approval. No access
// changes until an administrator closes the review.
func AccessReviewPreFillDef() feature.Def {
	return feature.Def{
		ID:      "F-ACC-03",
		Area:    "access",
		Name:    "Access review pre-fill",
		Source:  feature.SourceJob,
		Trigger: "schedule (quarterly)",
		Actions: []string{ActionPreFillReview},
		Tools: []string{
			"search_matters", "draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateReviewClose},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "documents.edit", "agent.ask"},
		Prompt:     promptFACC03,
		PromptPath: "prompts/f-acc-03.md",
		Fixture:    "testdata/ai/f-acc-03/access-review-pre-fill",
	}
}

// AccessReviewPreFillBinding binds F-ACC-03 to the quarterly schedule. A scheduled run acts
// for the feature's designated lawyer in firm settings and fails closed without one.
func AccessReviewPreFillBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(AccessReviewPreFillDef())
}
