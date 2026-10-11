package access_test

import (
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFACC07_DefMatchesSpec(t *testing.T) {
	accbCheckDefMatchesSpec(t, access.InviteListCheckDef())
}

// rule: AI-R-005
func TestFACC07_Prompt(t *testing.T) {
	accbCheckPrompt(t, access.InviteListCheckDef())
}

// The ok fixture pins the prompt hash, so a prompt or tool change is noticed.
func TestFACC07_FixtureHash(t *testing.T) {
	accbCheckFixtureHash(t, access.InviteListCheckDef())
}

// F-ACC-07 starts when an invite list is uploaded, acting for the event's user; the user-list
// screen runs it on request.
func TestFACC07_TriggerBinding(t *testing.T) {
	b, err := access.InviteListCheckBinding()
	accbBindingIs(t, b, err, access.FeatureInviteListCheck, access.EventInviteListUploaded)
	if got := access.InviteListCheckDef().Actions; !slices.Equal(got, []string{access.ActionPrecheckInviteList}) {
		t.Fatalf("actions = %v, want %s", got, access.ActionPrecheckInviteList)
	}
}

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFACC07_FromFeature(t *testing.T) {
	accbFromFeature(t, access.InviteListCheckDef(), "Invite list check: lst-synth-001",
		"Row 1 ada.synth@example.test: ready. Row 2 bo.synth@example.test: duplicate of an active user. Row 3 cy.synth@example.test: ready.")
}
