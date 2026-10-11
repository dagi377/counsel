package access_test

import (
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFACC10_DefMatchesSpec(t *testing.T) {
	accbCheckDefMatchesSpec(t, access.SuggestTeamDef())
}

// rule: AI-R-005
func TestFACC10_Prompt(t *testing.T) {
	accbCheckPrompt(t, access.SuggestTeamDef())
}

// The ok fixture pins the prompt hash, so a prompt or tool change is noticed.
func TestFACC10_FixtureHash(t *testing.T) {
	accbCheckFixtureHash(t, access.SuggestTeamDef())
}

// F-ACC-10 runs only from its screen action, matter-access > Suggest team.
func TestFACC10_TriggerBinding(t *testing.T) {
	d := access.SuggestTeamDef()
	tests := []struct {
		field     string
		got, want string
	}{
		{"source", string(d.Source), string(feature.SourceScreenAction)},
		{"trigger", d.Trigger, "on request: matter-access > Suggest team"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.field, tt.got, tt.want)
		}
	}
	if !slices.Equal(d.Actions, []string{"matter-access > Suggest team"}) {
		t.Fatalf("actions = %v, want matter-access > Suggest team", d.Actions)
	}
}

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFACC10_FromFeature(t *testing.T) {
	accbFromFeature(t, access.SuggestTeamDef(), "Proposed team: matter 2026-0601",
		"Lead: user-synthetic-lawyer (litigation, 4 open matters). Associate: user-synthetic-associate (litigation, 2 open matters).")
}
