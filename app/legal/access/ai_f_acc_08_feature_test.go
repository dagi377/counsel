package access_test

import (
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
)

// acc08Flow is the F-ACC-08 script: one search for the matters the answer names.
func acc08Flow() acccFlow {
	return acccFlow{
		def:    access.ExplainRoleDef(),
		prompt: "Explain role rol-synth-clerk: capabilities matters.view, time.record",
		calls:  []acccCall{acccSearch(acccQuery)},
		answer: "Role rol-synth-clerk can read matters and record time, as on 2026-0412 and 2026-0413. It cannot approve bills, move trust money or decide gates.",
		check: func(t *testing.T, ans string) {
			if !strings.HasPrefix(ans, "Role rol-synth-clerk ") || !strings.Contains(ans, " cannot ") {
				t.Fatalf("answer %q, want a plain summary of what the role can and cannot do", ans)
			}
		},
	}
}

// rule: AI-R-003
func TestFACC08_DefMatchesSpec(t *testing.T) {
	acccCheckDefMatchesSpec(t, access.ExplainRoleDef())
	acccCheckReadOnly(t, access.ExplainRoleDef())
}

// rule: AI-R-005
func TestFACC08_Prompt(t *testing.T) {
	acccCheckPrompt(t, access.ExplainRoleDef(), "Explain role", "Explain role on roles-list (Writes a plain summary of the role)", "You are read-only")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFACC08_FromFeature(t *testing.T) {
	acccRunFlow(t, acc08Flow())
}

// rule: AI-R-005
// The screen action runs inline through the production route and the answer is returned,
// never stored.
func TestFACC08_ScreenAction(t *testing.T) {
	if s := access.ExplainRoleDef().Source; s != "screen-action" {
		t.Fatalf("source = %s, want screen-action", s)
	}
	acccCheckRoute(t, acc08Flow())
}
