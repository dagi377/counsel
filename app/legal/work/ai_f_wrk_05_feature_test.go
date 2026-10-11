package work_test

import (
	"context"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/work"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
)

// rule: AI-R-003
func TestFWRK05_DefMatchesSpec(t *testing.T) {
	wrkdCheckSpec(t, work.DailyDigestDef())
}

func TestFWRK05_Prompt(t *testing.T) {
	wrkdCheckPrompt(t, work.DailyDigestDef(), "the daily work digest", "Expected output: digest (no gate)")
}

// F-WRK-05 runs every weekday morning for its designated lawyer; with none named it does not
// run and readiness flags it.
func TestFWRK05_TriggerBinding(t *testing.T) {
	b, err := work.DailyDigestBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	if b.FeatureID != "F-WRK-05" || b.Cadence != trigger.CadenceDaily || len(b.Events) != 0 || !b.Designated || b.ActingFor != "" {
		t.Fatalf("binding = %+v, want F-WRK-05 daily for its designated lawyer", b)
	}
	s := trigger.New(nil, clockfake.New(time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC), time.UTC), nil)
	if err := s.Add(b); err != nil {
		t.Fatalf("Add: %v", err)
	}
	unready, err := s.Readiness(context.Background())
	want := trigger.Unready{FeatureID: "F-WRK-05", Reason: trigger.ReasonNoDesignatedLawyer}
	if err != nil || len(unready) != 1 || unready[0] != want {
		t.Fatalf("readiness = %+v, %v; want %+v", unready, err, want)
	}
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFWRK05_FromFeature(t *testing.T) {
	wrkdCheckReadOnly(t, work.DailyDigestDef(), "Today: one open item on 2026-0405, the motion record review.")
}

// rule: AI-R-003
func TestFWRK05_AgentNeverApproves(t *testing.T) {
	wrkdCheckNoApproval(t, work.DailyDigestDef())
}
