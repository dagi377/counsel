package readiness_test

import (
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// rdybCheckScreenAction fails t unless d is a screen-action feature whose only action is its
// trigger's screen action, and otherwise matches its spec/ai/features.json entry. The spec
// words a screen action in trigger ("on request: <screen> > <action>") and leaves on_request
// empty, so the spec check runs on d without its own action.
func rdybCheckScreenAction(t *testing.T, d feature.Def, action string) {
	t.Helper()
	tests := []struct {
		name      string
		got, want string
	}{
		{"source", string(d.Source), string(feature.SourceScreenAction)},
		{"trigger", d.Trigger, "on request: " + action},
		{"actions", strings.Join(d.Actions, ";"), action},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
	spec := d
	spec.Actions = nil
	rdyCheckDefMatchesSpec(t, spec)
}
