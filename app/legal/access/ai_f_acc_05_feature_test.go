package access_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// acc05Alert is one alert line: the user, what happened, the matters.
var acc05Alert = regexp.MustCompile(`^alert: \S+: .+ on \d{4}-\d{4}(, \d{4}-\d{4})*$`)

// acc05Flow is the F-ACC-05 script: one search for the matters the batch names.
func acc05Flow() acccFlow {
	return acccFlow{
		def:    access.AnomalyWatchDef(),
		prompt: "New access events batch 2026-10-09 for review: usr-synth-clerk opened 2 matters in 10 minutes",
		calls:  []acccCall{acccSearch(acccQuery)},
		answer: "alert: usr-synth-clerk: opened 2 matters outside their assignments in 10 minutes on 2026-0412, 2026-0413",
		check: func(t *testing.T, ans string) {
			for _, l := range strings.Split(ans, "\n") {
				if !acc05Alert.MatchString(l) {
					t.Fatalf("alert line %q, want alert: user: what on matters", l)
				}
			}
		},
	}
}

// rule: AI-R-003
func TestFACC05_DefMatchesSpec(t *testing.T) {
	acccCheckDefMatchesSpec(t, access.AnomalyWatchDef())
	acccCheckReadOnly(t, access.AnomalyWatchDef())
}

// rule: AI-R-005
func TestFACC05_Prompt(t *testing.T) {
	acccCheckPrompt(t, access.AnomalyWatchDef(), "Anomaly watch", "Alert to owners", "You are read-only")
}

// F-ACC-05 starts on the access events, acting for the event's user; it has no schedule and
// no screen action.
func TestFACC05_TriggerBinding(t *testing.T) {
	b, err := access.AnomalyWatchBinding()
	acccCheckBinding(t, b, err, "F-ACC-05", []string{access.EventUserRoleChanged, access.EventUserDeactivated}, trigger.CadenceNone, false)
	if n := len(access.AnomalyWatchDef().Actions); n != 0 {
		t.Fatalf("actions = %d, want none", n)
	}
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFACC05_FromFeature(t *testing.T) {
	acccRunFlow(t, acc05Flow())
}
