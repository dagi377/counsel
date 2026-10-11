package docs

import (
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/deadline"
)

// Events that start F-DOC-06 outside its weekly slot: a known rule or form change.
const (
	// EventRulePackPublished is the kernel deadline event a published rule-pack version
	// emits: a known rule change.
	EventRulePackPublished = deadline.EventPackPublished
	// EventCourtFormChanged is the docs event a new court form version emits: a known form
	// change (docs.court_form).
	EventCourtFormChanged = "court_form.changed"
)

// TemplateCurrencyBinding binds F-DOC-06 to its weekly schedule and to a known rule or form
// change. A scheduled run acts for the feature's designated lawyer in firm settings, and does
// not start while none is named; an event run acts for the event's user.
//
// The run writes only through draft_document and submit_for_approval: each change request
// opens docs.template_publish (G-D2) and each stale flag docs.precedent (G-D3), with the
// acting user as preparer. A person other than the preparer decides them.
func TemplateCurrencyBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(TemplateCurrencyDef(), EventRulePackPublished, EventCourtFormChanged)
}
