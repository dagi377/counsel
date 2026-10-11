package conflict_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/conflict"
)

// rule: AI-R-003
func TestFCON12_DefMatchesSpec(t *testing.T) {
	conCheckDefMatchesSpec(t, conflict.DuplicateFinderDef())
}

// rule: AI-R-005
func TestFCON12_Prompt(t *testing.T) {
	conCheckPrompt(t, conflict.DuplicateFinderDef(), "Duplicate and relationship finder",
		"Suggestions on party-records-hygiene for lawyer merge approval", "read-only", "open no gate", "You never merge records")
}

// F-CON-12 runs nightly (a daily business-day cadence) for the user boot names; the
// party-records-hygiene screen runs it on request.
func TestFCON12_TriggerBinding(t *testing.T) {
	b, err := conflict.DuplicateFinderBinding("user-synthetic-records-clerk")
	conCheckBinding(t, b, err, "F-CON-12", nil, trigger.CadenceDaily, "user-synthetic-records-clerk")
	if _, err := conflict.DuplicateFinderBinding(""); !errors.Is(err, trigger.ErrBadBinding) {
		t.Fatalf("binding with no acting user = %v, want ErrBadBinding", err)
	}
	if got := conflict.DuplicateFinderDef().Actions; !slices.Equal(got, []string{conflict.ActionProposeDuplicates}) {
		t.Fatalf("actions = %v, want %s", got, conflict.ActionProposeDuplicates)
	}
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
// The result is a ranked list of suggestions with a citation for each, never a merge.
func TestFCON12_FromFeature(t *testing.T) {
	conCheckReadOnly(t, conflict.DuplicateFinderDef(), "1. Two party records share a registration number; likely duplicates.")
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFCON12_AgentNeverApproves(t *testing.T) {
	conCheckNoApproval(t, conflict.DuplicateFinderDef())
}
