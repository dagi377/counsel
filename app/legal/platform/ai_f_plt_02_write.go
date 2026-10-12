package platform

import (
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventEmailReceived is the a2 comms event published when an inbound email from a connected
// mailbox is stored (spec/eng/a2-interfaces.json, comms.EventEmailReceived). It starts
// F-PLT-02.
const EventEmailReceived = "email.received"

// MailFilingBinding binds F-PLT-02 to new mail arriving through a connector. An event run
// acts for the event's user, the mailbox owner, so it needs no scheduled acting user. The
// on-request starts are the Def's comms-log actions, "comms-log > Auto-file email to matter"
// and "comms-log > Propose time entry", served by trigger.Actions.
//
// The run writes only through gates, with the acting user as preparer: propose_deadline
// opens cal.confirm (G7) for each date the email sets, propose_time_entries opens
// timekeeping.accept (G9) for the time it records, and an email routed to the unfiled queue
// is drafted with draft_document and opened with submit_for_approval as
// docs.filing_privilege (G-D1). A person other than the preparer decides each one.
func MailFilingBinding() (trigger.Binding, error) {
	return trigger.Bind(MailFilingDef(), "", EventEmailReceived)
}
