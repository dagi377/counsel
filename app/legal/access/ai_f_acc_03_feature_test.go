package access_test

import (
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/identity"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFACC03_DefMatchesSpec(t *testing.T) {
	accCheckDefMatchesSpec(t, access.AccessReviewPreFillDef())
}

// rule: AI-R-005
func TestFACC03_Prompt(t *testing.T) {
	accCheckPrompt(t, access.AccessReviewPreFillDef(),
		"Access review pre-fill", "draft decisions for GA8", access.GateReviewClose, "report_plan", "draft_document")
}

// F-ACC-03 runs quarterly for its designated lawyer and fails closed without one; the
// access-review screen runs it on request.
func TestFACC03_TriggerBinding(t *testing.T) {
	b, err := access.AccessReviewPreFillBinding()
	accCheckBinding(t, b, err, "F-ACC-03", nil, trigger.CadenceQuarterly, true)
	if got := access.AccessReviewPreFillDef().Actions; !slices.Equal(got, []string{access.ActionPreFillReview}) {
		t.Fatalf("actions = %v, want %s", got, access.ActionPreFillReview)
	}
}

// acc03Flow drafts the review decisions and opens GA8. The review period and the matter
// number come from the matter the run reads.
var acc03Flow = accGateFlow{
	def:   access.AccessReviewPreFillDef(),
	kind:  access.GateReviewClose,
	dir:   "access-review-pre-fill",
	query: "access review",
	title: "Access review 2026-Q3: draft decisions",
	body:  "Matter 2026-0042 (last activity 2026-07-14): u-clerk-ada keep, active on the matter; u-clerk-bo revoke, no activity this quarter. An administrator closes the review.",
	final: "Drafted the access review decisions for an administrator.",
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFACC03_FromFeature(t *testing.T) {
	accRunFlow(t, acc03Flow, "Run the quarterly access review pre-fill for review 2026-Q3.",
		"access review 2026-Q3 last activity 2026-07-14",
		[]string{identity.KindUserActivation, identity.KindRoleChange, "trust.withdrawal"})
}
