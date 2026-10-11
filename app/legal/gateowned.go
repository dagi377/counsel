package legal

// rule: ARC-I-012
// GateOwned lists every gate-owned state key with the gate kind whose effect sets it. No route
// may declare one of these keys in its write declaration except the gate decision route; a
// route that wants one of these changes opens the gate and returns 202.
var GateOwned = map[string]string{
	"bill.status=approved":             "billing.bill_approval",
	"bill.number":                      "billing.bill_approval",
	"bill.status=voided":               "billing.bill_void",
	"bill.delivered_at":                "billing.bill_delivery",
	"credit_note.number":               "billing.credit_note",
	"credit_note.status=issued":        "billing.credit_note",
	"write_off.status=approved":        "billing.credit_note",
	"payment.allocation=applied":       "billing.payment_allocation",
	"payment.refund":                   "billing.refund",
	"trust_txn.posted=receipt":         "trust.receipt_classification",
	"trust_txn.posted=transfer":        "trust.transfer",
	"withdrawal.status=authorized":     "trust.withdrawal",
	"reconciliation.status=signed_off": "trust.reconciliation_signoff",
	"trust_account.signatories":        "core.trust_signatory",
	"matter.status=open":               "matter.opening",
	"matter.team":                      "matter.widen_access",
	"matter.restricted":                "matter.widen_access",
	"matter.status=closed":             "platform.closing",
	"deadline.status=confirmed":        "cal.confirm",
	"document.released":                "docs.outgoing",
	"user.roles":                       "core.role_change",
	"user.active":                      "core.user_activation",
	"webhook_endpoint.events":          "platform.integration_scope",
	"webhook_endpoint.active":          "platform.integration_scope",
}

// pendingGateKinds are GateOwned kinds that no module registers through Registry.GateKind
// yet. The set only shrinks: a kind leaves it in the change that registers it, and the
// conformance test fails while a registered kind is still listed.
var pendingGateKinds = map[string]bool{
	"billing.bill_approval":        true,
	"billing.bill_void":            true,
	"billing.bill_delivery":        true, // not in spec/gates.json yet (Q-HTTP-00)
	"billing.credit_note":          true,
	"billing.payment_allocation":   true,
	"billing.refund":               true,
	"trust.receipt_classification": true,
	"trust.transfer":               true,
	"trust.withdrawal":             true,
	"trust.reconciliation_signoff": true,
	"core.trust_signatory":         true,
	"matter.opening":               true,
	"matter.widen_access":          true,
	"platform.closing":             true,
	"cal.confirm":                  true,
	"docs.outgoing":                true,
	"core.role_change":             true,
	"core.user_activation":         true,
	"platform.integration_scope":   true,
}
