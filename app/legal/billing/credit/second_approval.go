package credit

import (
	"errors"
	"fmt"
)

// DefaultWriteOffSecondApprovalCents is the firm config writeoff_second_approval_cents
// default: a write-off above $1,000.00 needs a second approval by the managing lawyer.
const DefaultWriteOffSecondApprovalCents int64 = 100000

// ErrSecondApprovalRequired: a write-off above the firm threshold has no valid second
// approval by a managing lawyer.
var ErrSecondApprovalRequired = errors.New("credit: managing-lawyer second approval required")

// SecondApprovalConfig is the firm's write-off second-approval setting.
type SecondApprovalConfig struct {
	// WriteOffSecondApprovalCents is writeoff_second_approval_cents; zero means
	// DefaultWriteOffSecondApprovalCents.
	WriteOffSecondApprovalCents int64
}

// ThresholdCents returns the configured threshold, or the default when unset.
func (c SecondApprovalConfig) ThresholdCents() int64 {
	if c.WriteOffSecondApprovalCents <= 0 {
		return DefaultWriteOffSecondApprovalCents
	}
	return c.WriteOffSecondApprovalCents
}

// NeedsSecondApproval reports whether write-offs totalling amountCents need a second
// approval. Only a total strictly above the threshold does; exactly the threshold does not.
// The caller passes the cumulative total for the bill and client (WriteOffTotals).
//
// rule: BIL-C-085
func NeedsSecondApproval(amountCents int64, cfg SecondApprovalConfig) bool {
	// rule: BIL-C-085
	return amountCents > cfg.ThresholdCents()
}

// SecondApproval checks the managing lawyer's second approval of a write-off.
type SecondApproval struct {
	// ManagingLawyer reports whether userID holds the managing-lawyer capability (the firm
	// owner role, BIL-C-085). The caller resolves it from the role matrix. A nil func fails
	// closed: nobody qualifies.
	ManagingLawyer func(userID string) bool
}

// CheckSecondApprover rejects a second approval of w unless second is a human managing
// lawyer who is neither the first approver nor the preparer.
//
// rule: BIL-C-085
func (s SecondApproval) CheckSecondApprover(w WriteOff, first, second Approver) error {
	// rule: BIL-C-085
	// Above the threshold a managing lawyer approves a second time: never an agent, never
	// the preparer, never the lawyer who gave the first approval.
	switch {
	case second.IsAgent:
		return fmt.Errorf("second approval of write-off %s: %w", w.ID, ErrAgentApproval)
	case second.UserID == "":
		return fmt.Errorf("second approval of write-off %s: %w", w.ID, ErrSecondApprovalRequired)
	case second.UserID == w.PreparedBy || second.UserID == first.UserID:
		return fmt.Errorf("second approval of write-off %s: %w", w.ID, ErrSelfApproval)
	case first.IsAgent:
		return fmt.Errorf("first approval of write-off %s: %w", w.ID, ErrAgentApproval)
	case first.UserID == "" || first.UserID == w.PreparedBy:
		return fmt.Errorf("first approval of write-off %s: %w", w.ID, ErrSelfApproval)
	case s.ManagingLawyer == nil || !s.ManagingLawyer(second.UserID):
		return fmt.Errorf("second approval of write-off %s by %s: not a managing lawyer: %w", w.ID, second.UserID, ErrSecondApprovalRequired)
	}
	return nil
}
