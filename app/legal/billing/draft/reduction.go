package draft

import "fmt"

// ApplyReduction returns a copy of l with the write-down r set. It lowers the line's net
// only: AmountCents and SourceTimeEntryID are left as they are, so the source time entry is
// never changed. A reduction that would take the line's net below zero returns
// ErrNegativeLine; a missing reason returns ErrReasonRequired. On error l is returned as is.
//
// rule: BIL-C-023
// A write-down needs a reason code; "other" needs text.
func ApplyReduction(l Line, r Reduction) (Line, error) {
	if err := CheckReason(r); err != nil {
		return l, fmt.Errorf("reduce line %s: %w", l.ID, err)
	}
	out := l
	red := r
	out.Reduction = &red
	// rule: BIL-C-021
	// A reduction never makes a line negative.
	if LineNet(out) < 0 {
		return l, fmt.Errorf("reduce line %s by %d: %w", l.ID, r.Cents, ErrNegativeLine)
	}
	return out, nil
}
