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
func TestFTIM03_DefMatchesSpec(t *testing.T) {
	timbCheckDefMatchesSpec(t, tk.PreBillCheckDef())
}

// rule: AI-R-005
func TestFTIM03_Prompt(t *testing.T) {
	timbCheckPrompt(t, tk.PreBillCheckDef(),
		"Pre-bill check", "WriteDown proposals for G9d", tk.GateWriteDown,
		"report_plan", "draft_document", "submit_for_approval")
}

// F-TIM-03 starts when a draft bill is requested, acting for the event's user, and monthly,
// acting for the designated lawyer in firm settings; matter-wip and write-downs run it on
// request.
func TestFTIM03_TriggerBinding(t *testing.T) {
	b, err := tk.PreBillCheckBinding()
	timbCheckBinding(t, b, err, tk.PreBillCheckDef(), []string{tk.EventBillDraftRequested},
		trigger.CadenceMonthly, "", tk.ActionPreBillCheck, tk.ActionProposeReduction)
	// The monthly run acts for the designated lawyer, never a fixed user: with no designated
	// lawyer the feature is unready and does not start.
	if !b.Designated || b.ActingFor != "" {
		t.Fatalf("binding = %+v, want designated with no fixed acting user", b)
	}
	s := trigger.New(nil, nil, nil)
	if err := s.Add(b); err != nil {
		t.Fatalf("Add: %v", err)
	}
	unready, err := s.Readiness(context.Background())
	want := trigger.Unready{FeatureID: b.FeatureID, Reason: trigger.ReasonNoDesignatedLawyer}
	if err != nil || len(unready) != 1 || unready[0] != want {
		t.Fatalf("readiness = %+v, %v; want %+v", unready, err, want)
	}
}

// tim03Run seeds a matter whose unbilled entry has narrative, then runs F-TIM-03 on a bill
// draft request with a script that drafts a write-down and submits it under gateKind.
func tim03Run(t *testing.T, narrative, gateKind string) (timbWorld, agenttest.FeatureRun, []timbCall) {
	t.Helper()
	w := timbFeature(t, tk.PreBillCheckDef(), narrative)
	calls := timbDocCalls(w, "Write-down proposals before billing",
		"Entry te-1: amount 45000 cents. Reduce by 15000 cents: the narrative is block-billed and the task called for one hour.", gateKind)
	model := timbScript("F-TIM-03", calls, "Proposed one write-down for approval.")
	run := w.fd.Fire(context.Background(), model, "run-ftim03", "Bill draft requested for the matter.")
	return w, run, calls
}

// rule: ARC-I-035
// rule: ARC-I-039
func TestFTIM03_FromFeature(t *testing.T) {
	w, run, calls := tim03Run(t, "Reviewed file; drafted motion; calls", tk.GateWriteDown)
	timbCheckRun(t, w, run, calls, tk.GateWriteDown)
}

// rule: ARC-I-035
// timekeeping.writedown is a tool-gap kind: draft_document then submit_for_approval opens
// it, and submit_for_approval may open no kind outside the Def.
func TestFTIM03_GateKindNotAllowed(t *testing.T) {
	for _, kind := range []string{"timekeeping.disbursement", "timekeeping.timesheet", "billing.bill_approval", "trust.withdrawal"} {
		t.Run(kind, func(t *testing.T) {
			w, run, _ := tim03Run(t, "Reviewed file; drafted motion; calls", kind)
			timbCheckKindRefused(t, w, run)
		})
	}
}

// rule: ARC-I-038
func TestFTIM03_Injection(t *testing.T) {
	w, run, calls := tim03Run(t, "Reviewed file. "+timbInjected, tk.GateWriteDown)
	timbCheckInjection(t, w, run, calls, timbInjected)
}
