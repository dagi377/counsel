package timekeeping

import "fmt"

// Write-down kinds and stages (timekeeping.write_down.kind and .stage).
const (
	KindWriteDown = "write_down"
	KindWriteUp   = "write_up"
	StagePreBill  = "pre_bill"
	StagePostBill = "post_bill"
)

// WriteDownThresholdCents is the firm threshold (BIL-C-013 config writedown_threshold_cents):
// a write-down larger than this also needs the owner role.
const WriteDownThresholdCents int64 = 50000

// LineAdjustment is what a checked write-down does: it sets the bill line amount and leaves
// the entry's minutes as they were.
type LineAdjustment struct {
	Entry           TimeEntry // unchanged: Minutes and BilledMinutes stay the accepted record
	LineAmountCents int64     // the bill line amount after the adjustment
	Gate            Gate      // G9d off a bill, G-B1 while on a draft bill
	NeedsOwnerRole  bool      // the write-down is above WriteDownThresholdCents
}

// CheckWriteDown validates w against entry e and returns the adjusted bill line amount.
// rule: BIL-C-013
// TimeEntry.minutes and billed_minutes are the accepted record; a WriteDown row changes the
// bill line amount only. Pre-bill write-down: G9d / G-B1; above the firm threshold also the
// owner role.
func CheckWriteDown(e TimeEntry, w WriteDown) (LineAdjustment, error) {
	switch {
	case w.ProposedBy.Agent:
		return LineAdjustment{}, fmt.Errorf("write-down %s: %w", w.EntryID, ErrAgentForbidden)
	case w.EntryID == "" || w.EntryID != e.ID:
		return LineAdjustment{}, fmt.Errorf("write-down %s on entry %s: %w", w.EntryID, e.ID, ErrInvalid)
	case w.OriginalCents < 0 || w.AdjustedCents < 0:
		return LineAdjustment{}, fmt.Errorf("write-down %s: negative amount: %w", w.EntryID, ErrInvalid)
	case w.ReasonCode == "":
		return LineAdjustment{}, fmt.Errorf("write-down %s: reason code required: %w", w.EntryID, ErrInvalid)
	}
	switch w.Kind {
	case KindWriteDown:
		if w.AdjustedCents > w.OriginalCents {
			return LineAdjustment{}, fmt.Errorf("write-down %s raises the amount: %w", w.EntryID, ErrInvalid)
		}
	case KindWriteUp:
		if w.AdjustedCents < w.OriginalCents {
			return LineAdjustment{}, fmt.Errorf("write-up %s lowers the amount: %w", w.EntryID, ErrInvalid)
		}
	default:
		return LineAdjustment{}, fmt.Errorf("write-down %s kind %q: %w", w.EntryID, w.Kind, ErrInvalid)
	}
	gate := GateG9d
	switch w.Stage {
	case StagePreBill:
		if e.State == StateBilled {
			return LineAdjustment{}, fmt.Errorf("pre-bill write-down %s on a billed entry: %w", w.EntryID, ErrBadTransition)
		}
		if e.State == StateOnDraftBill {
			gate = GateGB1
		}
	case StagePostBill:
		if e.State != StateBilled {
			return LineAdjustment{}, fmt.Errorf("post-bill write-down %s on entry in %s: %w", w.EntryID, e.State, ErrBadTransition)
		}
	default:
		return LineAdjustment{}, fmt.Errorf("write-down %s stage %q: %w", w.EntryID, w.Stage, ErrInvalid)
	}
	return LineAdjustment{
		Entry:           e,
		LineAmountCents: w.AdjustedCents,
		Gate:            gate,
		NeedsOwnerRole:  w.Kind == KindWriteDown && w.OriginalCents-w.AdjustedCents > WriteDownThresholdCents,
	}, nil
}

// CheckEditable says whether e may be edited. An entry on a non-void bill is locked; it changes
// only through a write-down. ErrBilledLocked maps to 409.
// rule: BIL-C-013
func CheckEditable(e TimeEntry) error {
	if e.BillID != "" || e.State == StateOnDraftBill || e.State == StateBilled {
		return fmt.Errorf("edit entry %s: %w", e.ID, ErrBilledLocked)
	}
	return nil
}
