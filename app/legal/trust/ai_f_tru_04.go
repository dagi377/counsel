package trust

import (
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureMonthEndReconciliation is the spec id of "month-end-reconciliation".
const FeatureMonthEndReconciliation = "F-TRU-04"

// EventStatementReceived is the event of a trust account's bank statement for a closed month
// being received (docs/QUESTIONS.md Q-FEAT-TRU-04).
const EventStatementReceived = "trust.statement_received"

// GateSignOff is G12, recon.GateSignOff. recon imports trust, so the kind is
// restated here; a test holds the two equal.
const GateSignOff = "trust.reconciliation_signoff"

// MonthEndReconciliationDef is F-TRU-04: once the bank statement for a closed month is
// received, the agent reads each matter's trust balance and the bills it touches, drafts the
// three-way reconciliation package with draft_document and submits it with
// submit_for_approval for trust.reconciliation_signoff (G12). A lawyer other than the
// preparer signs it off; the agent never signs off a reconciliation.
func MonthEndReconciliationDef() feature.Def {
	return feature.Def{
		ID:      FeatureMonthEndReconciliation,
		Area:    "trust",
		Name:    "month-end-reconciliation",
		Source:  feature.SourceJob,
		Trigger: "schedule (statement received after month end)",
		Actions: []string{"monthly-reconciliation > Prepare package"},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "get_trust_balance", "get_bill",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// trust.reconciliation_signoff has no dedicated write tool: the package is drafted and
		// submitted, never signed.
		GateKinds:  []string{GateSignOff},
		Grant:      []string{"agent.ask", "billing.view", "documents.edit", "matters.view", "trust.view"},
		Prompt:     prompts,
		PromptPath: "prompts/f-tru-04.md",
		Fixture:    "testdata/ai/f-tru-04/month-end-reconciliation",
	}
}

// MonthEndReconciliationBinding binds F-TRU-04 to a received bank statement and to a daily
// check, so a statement that arrived while the firm was closed is still reconciled. The
// scheduled run acts for the firm's designated trust lawyer (JOB-ACTOR); an event run acts
// for the user who filed the statement.
func MonthEndReconciliationBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(MonthEndReconciliationDef(), EventStatementReceived)
}
