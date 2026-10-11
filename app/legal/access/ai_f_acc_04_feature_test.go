package access_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/identity"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFACC04_DefMatchesSpec(t *testing.T) {
	accCheckDefMatchesSpec(t, access.BreakGlassSummaryDef())
}

// rule: AI-R-005
func TestFACC04_Prompt(t *testing.T) {
	accCheckPrompt(t, access.BreakGlassSummaryDef(),
		"Break-glass summary", "summary for GA7", access.GateBreakGlassReview, "report_plan", "draft_document")
}

// F-ACC-04 starts when a break-glass session ends, acting for the event's user; it has no
// schedule and no screen action.
func TestFACC04_TriggerBinding(t *testing.T) {
	b, err := access.BreakGlassSummaryBinding()
	accCheckBinding(t, b, err, "F-ACC-04", []string{access.EventBreakGlassEnded}, trigger.CadenceNone, false)
	if got := access.BreakGlassSummaryDef().Actions; len(got) != 0 {
		t.Fatalf("actions = %v, want none", got)
	}
}

// acc04Flow drafts the session summary and opens GA7. The session date and the matter
// number come from the matter the run reads.
var acc04Flow = accGateFlow{
	def:   access.BreakGlassSummaryDef(),
	kind:  access.GateBreakGlassReview,
	dir:   "break-glass-summary",
	query: "break-glass",
	title: "Break-glass session bg-0042: summary",
	body:  "Session bg-0042 on 2026-10-09 opened matter 2026-0042; documents were viewed and nothing was changed. An administrator reviews the session.",
	final: "Drafted the break-glass summary for an administrator.",
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFACC04_FromFeature(t *testing.T) {
	accRunFlow(t, acc04Flow, "The break-glass session ended for session bg-0042.",
		"break-glass session bg-0042 on 2026-10-09",
		[]string{access.GateReviewClose, identity.KindUserActivation, "trust.withdrawal"})
}
