package intake

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dagi377/counsel/kernel/agenttools"
)

// triageTitle is the title of the drafted triage card. It is fixed feature text, so the
// draft's title carries no lead content.
const triageTitle = "Lead triage card"

// TriageDraftInput is the draft_document input that files card on matterID: one line per
// item with its kind, its text and the version, page and lines it cites. The triage card is a
// tool-gap write: the draft opens no gate itself, TriageSubmitInput then opens G1
// (intake.lead_decision) on it with the acting user as preparer. A card with no item, an item
// with no text or an invalid citation is ErrBadTriageItem, and nothing is drafted.
func TriageDraftInput(matterID string, card TriageCard) (json.RawMessage, error) {
	if matterID == "" || card.LeadID == "" || len(card.Items) == 0 {
		return nil, fmt.Errorf("triage draft: matter, lead and items required: %w", ErrBadTriageItem)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Lead %s\n\n", card.LeadID)
	for i, it := range card.Items {
		if strings.TrimSpace(it.Text) == "" {
			return nil, fmt.Errorf("triage draft: item %d: empty: %w", i, ErrBadTriageItem)
		}
		// rule: AI-R-003
		// Every line of the card keeps the citation the extraction checked.
		if err := agenttools.ValidCitation(it.Citation); err != nil {
			return nil, fmt.Errorf("triage draft: item %d: %w: %w", i, ErrBadTriageItem, err)
		}
		c := it.Citation
		fmt.Fprintf(&b, "- %s: %s (source %s, page %d, lines %d-%d)\n", it.Kind, it.Text, c.VersionID, c.Page, c.FromLine, c.ToLine)
	}
	in, err := json.Marshal(map[string]any{"matter_id": matterID, "title": triageTitle, "body_markdown": b.String()})
	if err != nil {
		return nil, fmt.Errorf("triage draft: %w", err)
	}
	return in, nil
}

// TriageSubmitInput is the submit_for_approval input that opens G1 (intake.lead_decision) on
// the triage card draftID. It names no other gate kind; the run scope refuses any other.
func TriageSubmitInput(draftID string) (json.RawMessage, error) {
	if draftID == "" {
		return nil, fmt.Errorf("triage submit: draft id required: %w", ErrBadTriageItem)
	}
	// rule: ARC-I-035
	in, err := json.Marshal(map[string]string{"subject_type": "document", "subject_id": draftID, "gate_kind": GateLeadDecision})
	if err != nil {
		return nil, fmt.Errorf("triage submit: %w", err)
	}
	return in, nil
}
