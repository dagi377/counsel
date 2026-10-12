package access_test

import (
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
)

// acc09Flow is the F-ACC-09 script: one search for the matters the answer names.
func acc09Flow() acccFlow {
	return acccFlow{
		def:    access.ExplainAccessDef(),
		prompt: "Explain access usr-synth-viewer: roles rol-synth-associate; matter 2026-0412",
		calls:  []acccCall{acccSearch(acccQuery)},
		answer: "usr-synth-viewer can see 2026-0412 because they are on its matter team as associate.",
		check: func(t *testing.T, ans string) {
			if !strings.HasPrefix(ans, "usr-synth-viewer can see 2026-0412 because ") {
				t.Fatalf("answer %q, want why the person can see the matter", ans)
			}
		},
	}
}

// rule: AI-R-003
func TestFACC09_DefMatchesSpec(t *testing.T) {
	acccCheckDefMatchesSpec(t, access.ExplainAccessDef())
	acccCheckReadOnly(t, access.ExplainAccessDef())
}

// rule: AI-R-005
func TestFACC09_Prompt(t *testing.T) {
	acccCheckPrompt(t, access.ExplainAccessDef(), "Explain access", "Explain access on user-detail (Answers why can this person see matter X)", "You are read-only")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFACC09_FromFeature(t *testing.T) {
	acccRunFlow(t, acc09Flow())
}

// rule: AI-R-005
// The screen action runs inline through the production route and the answer is returned,
// never stored.
func TestFACC09_ScreenAction(t *testing.T) {
	if s := access.ExplainAccessDef().Source; s != "screen-action" {
		t.Fatalf("source = %s, want screen-action", s)
	}
	acccCheckRoute(t, acc09Flow())
}
