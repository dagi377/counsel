package intake

import "github.com/dagi377/counsel/app/legal/party"

// Submission and Lead are read by party through its own small interfaces, so party never
// imports intake and intake may import party.
var (
	_ party.Submission = Submission{}
	_ party.Lead       = Lead{}
)

// SubmittedContactName is the contact name as typed on the form (party.Submission).
func (s Submission) SubmittedContactName() string { return s.ContactName }

// LeadContact returns the lead id and its party.contact id (party.Lead).
func (l Lead) LeadContact() (leadID, contactID string) { return l.ID, l.ContactID }
