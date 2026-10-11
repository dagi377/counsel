package work_test

import (
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/work"
)

// rule: AI-R-003
func TestFWRK08_DefMatchesSpec(t *testing.T) {
	wrkdCheckSpec(t, work.ExplainAutomationRunsDef())
}

func TestFWRK08_Prompt(t *testing.T) {
	wrkdCheckPrompt(t, work.ExplainAutomationRunsDef(), "explain the automation run on its run history", "Expected output: explanation on run history (no gate)")
}

// F-WRK-08 starts on a failed or a skipped automation run and acts for the event's user: no
// schedule and no designated lawyer.
func TestFWRK08_TriggerBinding(t *testing.T) {
	b, err := work.ExplainAutomationRunsBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	want := []string{work.EventAutomationRunFailed, work.EventAutomationRunSkipped}
	if b.FeatureID != "F-WRK-08" || b.Cadence != trigger.CadenceNone || b.Designated || b.ActingFor != "" || !slices.Equal(b.Events, want) {
		t.Fatalf("binding = %+v, want F-WRK-08 on %v for the event's user", b, want)
	}
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFWRK08_FromFeature(t *testing.T) {
	wrkdCheckReadOnly(t, work.ExplainAutomationRunsDef(), "The run was skipped: the motion record review on 2026-0405 is still open.")
}

// rule: AI-R-003
func TestFWRK08_AgentNeverApproves(t *testing.T) {
	wrkdCheckNoApproval(t, work.ExplainAutomationRunsDef())
}
