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
func TestFTIM02_DefMatchesSpec(t *testing.T) {
	timbCheckDefMatchesSpec(t, tk.ReceiptCaptureDef())
}

// rule: AI-R-005
func TestFTIM02_Prompt(t *testing.T) {
	timbCheckPrompt(t, tk.ReceiptCaptureDef(),
		"Receipt capture", "draft Expense for G9b", tk.GateDisbursement,
		"report_plan", "draft_document", "submit_for_approval")
}

// F-TIM-02 starts on a receipt uploaded or emailed to a matter, acting for the event's user;
// the expenses screen runs it on request.
func TestFTIM02_TriggerBinding(t *testing.T) {
	b, err := tk.ReceiptCaptureBinding()
	timbCheckBinding(t, b, err, tk.ReceiptCaptureDef(),
		[]string{tk.EventDocumentUploaded, tk.EventEmailFiled}, trigger.CadenceNone, "", tk.ActionExtractFromReceipt)
}

// tim02Run seeds a matter whose unbilled entry has narrative, then runs F-TIM-02 on a
// receipt upload with a script that drafts the expense and submits it under gateKind.
func tim02Run(t *testing.T, narrative, gateKind string) (timbWorld, agenttest.FeatureRun, []timbCall) {
	t.Helper()
	w := timbFeature(t, tk.ReceiptCaptureDef(), narrative)
	calls := timbDocCalls(w, "Draft expense: courier receipt",
		"Date: 2026-10-02. Payee: Synthetic Courier. Description: delivery of the motion record. Amount: 4500 cents. Tax: 585 cents. Billable: yes.", gateKind)
	model := timbScript("F-TIM-02", calls, "Drafted the expense for approval.")
	run := w.fd.Fire(context.Background(), model, "run-ftim02",
		"Receipt uploaded: Synthetic Courier, 2026-10-02, delivery of the motion record, 45.00 plus 5.85 HST.")
	return w, run, calls
}

// rule: ARC-I-035
// rule: ARC-I-039
func TestFTIM02_FromFeature(t *testing.T) {
	w, run, calls := tim02Run(t, "Drafted the motion record", tk.GateDisbursement)
	timbCheckRun(t, w, run, calls, tk.GateDisbursement)
}

// rule: ARC-I-035
// timekeeping.disbursement is a tool-gap kind: draft_document then submit_for_approval opens
// it, and submit_for_approval may open no kind outside the Def.
func TestFTIM02_GateKindNotAllowed(t *testing.T) {
	for _, kind := range []string{"timekeeping.writedown", "timekeeping.timesheet", "billing.bill_approval", "trust.withdrawal"} {
		t.Run(kind, func(t *testing.T) {
			w, run, _ := tim02Run(t, "Drafted the motion record", kind)
			timbCheckKindRefused(t, w, run)
		})
	}
}

// rule: ARC-I-038
func TestFTIM02_Injection(t *testing.T) {
	w, run, calls := tim02Run(t, "Drafted the motion record. "+timbInjected, tk.GateDisbursement)
	timbCheckInjection(t, w, run, calls, timbInjected)
}
