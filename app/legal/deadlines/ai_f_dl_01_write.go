package deadlines

import (
	"encoding/json"
	"fmt"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// EventDocumentUploaded is the a2 docs event a filed document publishes
// (spec/eng/a2-interfaces.json events: document.uploaded, with matter_id). It starts F-DL-01.
const EventDocumentUploaded = "document.uploaded"

// ExtractDeadlinesBinding binds F-DL-01 to a document filed to a matter. Event runs act for
// the event's user, so it needs no scheduled acting user. The on-request start is the Def's
// screen action, "matter-deadlines > Extract from document", served by trigger.Actions.
func ExtractDeadlinesBinding() (trigger.Binding, error) {
	return trigger.Bind(ExtractDeadlinesDef(), "", EventDocumentUploaded)
}

// ProposeInput is the propose_deadline input for one extracted deadline on matterID, citing
// source. The write goes through propose_deadline, which opens cal.confirm with the acting
// user as preparer and refuses a due date the run did not read (compute_deadline or a cited
// document date). An item with no stated due date must be computed first: it is refused here
// rather than given a date the model worked out.
func ProposeInput(matterID string, it ProposedDeadline, docID, quoteHash string) (json.RawMessage, error) {
	if matterID == "" || it.Title == "" {
		return nil, fmt.Errorf("propose input: matter and title required: %w", ErrBadProposal)
	}
	// rule: ARC-I-035
	// The due date is only ever one the run read; this never derives one from a period.
	if it.DueDate == "" {
		return nil, fmt.Errorf("propose input: no due date, compute_deadline first: %w", ErrBadProposal)
	}
	in := map[string]any{
		"matter_id": matterID,
		"title":     it.Title,
		"due_on":    it.DueDate,
		"source": map[string]any{
			"doc_id":     docID,
			"version_id": it.Citation.VersionID,
			"page":       it.Citation.Page,
			"line_start": it.Citation.FromLine,
			"line_end":   it.Citation.ToLine,
			"quote_hash": quoteHash,
		},
	}
	if it.TriggerDate != "" {
		in["trigger_date"] = it.TriggerDate
	}
	b, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("propose input: %w", err)
	}
	return b, nil
}
