package docs

import (
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventDocumentUploaded is the a2 event the docs module publishes when a document version is
// filed on a matter. Every intake channel F-DOC-01 names (upload, email-in, portal upload and
// scan) files the document through the same version store, so this one event starts it.
const EventDocumentUploaded = "document.uploaded"

// IngestDocumentBinding binds F-DOC-01 to EventDocumentUploaded. An event run acts for the
// event's user, so it needs no scheduled acting user.
//
// The run writes only through gates, with the acting user as preparer: propose_deadline opens
// cal.confirm (G7) for each date the document sets, and a filing that needs review is drafted
// with draft_document and opened with submit_for_approval as docs.filing_privilege (G-D1). A
// person other than the preparer decides them.
func IngestDocumentBinding() (trigger.Binding, error) {
	return trigger.Bind(IngestDocumentDef(), "", EventDocumentUploaded)
}
