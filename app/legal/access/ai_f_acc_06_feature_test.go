package access_test

import (
	"os"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFACC06_DefMatchesSpec(t *testing.T) {
	accbCheckDefMatchesSpec(t, access.OffboardingPlanDef())
}

// rule: AI-R-005
func TestFACC06_Prompt(t *testing.T) {
	accbCheckPrompt(t, access.OffboardingPlanDef())
}

// The ok fixture pins the prompt hash, so a prompt or tool change is noticed.
func TestFACC06_FixtureHash(t *testing.T) {
	accbCheckFixtureHash(t, access.OffboardingPlanDef())
	if err := feature.LintFixtureHash(access.OffboardingPlanDef(), os.DirFS("../../..")); err != nil {
		t.Fatal(err)
	}
}

// F-ACC-06 starts when an offboarding starts, acting for the event's user; the offboard-user
// screen runs it on request.
func TestFACC06_TriggerBinding(t *testing.T) {
	b, err := access.OffboardingPlanBinding()
	accbBindingIs(t, b, err, access.FeatureOffboardingPlan, access.EventOffboardStarted)
	if got := access.OffboardingPlanDef().Actions; !slices.Equal(got, []string{access.ActionProposeSuccessors}) {
		t.Fatalf("actions = %v, want %s", got, access.ActionProposeSuccessors)
	}
}

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFACC06_FromFeature(t *testing.T) {
	accbFromFeature(t, access.OffboardingPlanDef(), "Handover: user-synthetic-leaver",
		"Matter 2026-0601: successor user-synthetic-successor (same practice area, lightest workload). Open work: the pending court filing.")
}
