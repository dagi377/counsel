package conflict_test

import (
	"os"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/conflict"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFCON03_DefMatchesSpec(t *testing.T) {
	conSpecMatches(t, conflict.ClearanceMemoDef())
}

// rule: AI-R-005
func TestFCON03_Prompt(t *testing.T) {
	conPromptLints(t, conflict.ClearanceMemoDef(), "Clearance memo", "Draft memo for gate G2")
}

// The ok fixture pins the prompt hash, so a prompt or tool change is noticed.
func TestFCON03_FixtureHash(t *testing.T) {
	d := conflict.ClearanceMemoDef()
	if err := feature.LintFixtureHash(d, os.DirFS("../../..")); err != nil {
		t.Fatal(err)
	}
}

func TestFCON03_TriggerBinding(t *testing.T) {
	b, err := conflict.ClearanceMemoBinding()
	conBindingIs(t, b, err, "F-CON-03", conflict.EventHitsDisposed)
}

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFCON03_FromFeature(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inject bool
	}{{"ok", false}, {"injection", true}} {
		t.Run(tc.name, func(t *testing.T) {
			conRun(t, conflict.ClearanceMemoDef(), tc.inject)
		})
	}
}
