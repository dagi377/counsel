package timekeeping_test

import (
	"context"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFTIM04_DefMatchesSpec(t *testing.T) {
	timbCheckDefMatchesSpec(t, tk.TimesheetPreReviewDef())
}

// rule: AI-R-005
func TestFTIM04_Prompt(t *testing.T) {
	timbCheckPrompt(t, tk.TimesheetPreReviewDef(),
		"Timesheet pre-review", "flags for G9c", tk.GateTimesheet,
		"report_plan", "draft_document", "submit_for_approval")
}

// F-TIM-04 starts when a week is submitted, acting for the event's user; no screen runs it
// on request.
func TestFTIM04_TriggerBinding(t *testing.T) {
	b, err := tk.TimesheetPreReviewBinding()
	timbCheckBinding(t, b, err, tk.TimesheetPreReviewDef(),
		[]string{tk.EventTimesheetWeekSubmitted}, trigger.CadenceNone, "")
}

// tim04Run seeds a matter whose unbilled entry has narrative, then runs F-TIM-04 on a
// submitted week with a script that drafts the flags and submits them under gateKind.
func tim04Run(t *testing.T, narrative, gateKind string) (timbWorld, agenttest.FeatureRun, []timbCall) {
	t.Helper()
	w := timbFeature(t, tk.TimesheetPreReviewDef(), narrative)
	calls := timbDocCalls(w, "Timesheet flags for review",
		"Entry te-1: 90 minutes, 45000 cents. Flag: the narrative is vague and names no task.", gateKind)
	model := timbScript("F-TIM-04", calls, "Flagged one entry for the reviewer.")
	run := w.fd.Fire(context.Background(), model, "run-ftim04", "Week submitted for approval.")
	return w, run, calls
}

// rule: ARC-I-035
// rule: ARC-I-039
func TestFTIM04_FromFeature(t *testing.T) {
	w, run, calls := tim04Run(t, "Work on file", tk.GateTimesheet)
	timbCheckRun(t, w, run, calls, tk.GateTimesheet)
}

// rule: ARC-I-035
// timekeeping.timesheet is a tool-gap kind: draft_document then submit_for_approval opens
// it, and submit_for_approval may open no kind outside the Def.
func TestFTIM04_GateKindNotAllowed(t *testing.T) {
	for _, kind := range []string{"timekeeping.disbursement", "timekeeping.writedown", "timekeeping.accept", "trust.withdrawal"} {
		t.Run(kind, func(t *testing.T) {
			w, run, _ := tim04Run(t, "Work on file", kind)
			timbCheckKindRefused(t, w, run)
		})
	}
}

// rule: ARC-I-038
func TestFTIM04_Injection(t *testing.T) {
	w, run, calls := tim04Run(t, "Work on file. "+timbInjected, tk.GateTimesheet)
	timbCheckInjection(t, w, run, calls, timbInjected)
}
