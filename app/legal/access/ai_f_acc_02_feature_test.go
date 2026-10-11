package access_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/identity"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFACC02_DefMatchesSpec(t *testing.T) {
	accCheckDefMatchesSpec(t, access.LicenceCheckDef())
}

// rule: AI-R-005
func TestFACC02_Prompt(t *testing.T) {
	accCheckPrompt(t, access.LicenceCheckDef(),
		"Licence check", "warning and automatic downgrade proposal for GA2", identity.KindUserActivation, "report_plan", "draft_document")
}

// F-ACC-02 starts on an invite, acting for the inviting user, and monthly for its designated
// lawyer; with no designated lawyer the scheduled run fails closed.
func TestFACC02_TriggerBinding(t *testing.T) {
	b, err := access.LicenceCheckBinding()
	accCheckBinding(t, b, err, "F-ACC-02", []string{access.EventUserInvited}, trigger.CadenceMonthly, true)
	if got := access.LicenceCheckDef().Actions; len(got) != 0 {
		t.Fatalf("actions = %v, want none", got)
	}
}

// acc02Flow drafts the licence warning and downgrade proposal and opens GA2. The seat counts
// and the review date come from the matter the run reads.
var acc02Flow = accGateFlow{
	def:   access.LicenceCheckDef(),
	kind:  identity.KindUserActivation,
	dir:   "licence-check",
	query: "licence",
	title: "Licence warning: 12 seats in use, 10 licensed",
	body:  "Seats in use 12, licensed 10 (licence review 2026-10-01). Proposed downgrade: two inactive seats to read-only. Each change waits on an administrator.",
	final: "Drafted the licence warning and downgrade proposal for an administrator.",
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFACC02_FromFeature(t *testing.T) {
	accRunFlow(t, acc02Flow, "Monthly licence check: 12 seats in use, 10 licensed.",
		"Firm licence 12 seats 10 licensed review 2026-10-01",
		[]string{identity.KindRoleChange, "core.review_close", "trust.withdrawal"})
}
