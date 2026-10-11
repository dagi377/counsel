package access_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// acc01Breach is one breach line: hard or soft, the rule, the user or role, the matter.
var acc01Breach = regexp.MustCompile(`^(hard|soft): [^:]+: \S+ on \d{4}-\d{4}$`)

// acc01Flow is the F-ACC-01 script: one search for the matters the changed roles touch.
func acc01Flow() acccFlow {
	return acccFlow{
		def:    access.DutyScanDef(),
		prompt: "Duty scan: role change user usr-synth-clerk roles rol-synth-bookkeeper, rol-synth-trust-approver",
		calls:  []acccCall{acccSearch(acccQuery)},
		answer: "hard: records and reconciles the trust account: usr-synth-clerk on 2026-0412\n" +
			"soft: bills and approves bills: rol-synth-bookkeeper on 2026-0413",
		check: func(t *testing.T, ans string) {
			for _, l := range strings.Split(ans, "\n") {
				if !acc01Breach.MatchString(l) {
					t.Fatalf("breach line %q, want hard|soft: rule: id on matter", l)
				}
			}
		},
	}
}

// rule: AI-R-003
func TestFACC01_DefMatchesSpec(t *testing.T) {
	acccCheckDefMatchesSpec(t, access.DutyScanDef())
	acccCheckReadOnly(t, access.DutyScanDef())
}

// rule: AI-R-005
func TestFACC01_Prompt(t *testing.T) {
	acccCheckPrompt(t, access.DutyScanDef(), "Duty scan", "Breach list for duty-rules; blocks GA1 submit on hard conflicts", "You are read-only")
}

// F-ACC-01 runs daily for the designated lawyer, on user.role_changed for the event's user,
// and on request from role-editor and duty-rules.
func TestFACC01_TriggerBinding(t *testing.T) {
	b, err := access.DutyScanBinding()
	acccCheckBinding(t, b, err, "F-ACC-01", []string{access.EventUserRoleChanged}, trigger.CadenceDaily, true)
	if got := strings.Join(access.DutyScanDef().Actions, ";"); got != access.ActionCheckDuties+";"+access.ActionScanDuties {
		t.Fatalf("actions = %s", got)
	}
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFACC01_FromFeature(t *testing.T) {
	acccRunFlow(t, acc01Flow())
}

// rule: AI-R-005
// The on-request scans run inline through the screen-action route and answer only.
func TestFACC01_ScreenActions(t *testing.T) {
	acccCheckRoute(t, acc01Flow())
}
