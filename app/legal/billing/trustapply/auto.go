package trustapply

import (
	"context"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags"
)

// StatusProposed is the only state auto-apply creates a trust fee transfer in.
const StatusProposed = "proposed"

// AutoApplyGates are the gates a proposed trust fee transfer still needs before trust
// money moves: G-B4 (authorize) and G11 (execute).
var AutoApplyGates = []string{"G-B4", "G11"}

// Proposal is a trust fee transfer recorded in StatusProposed.
type Proposal struct {
	ID       string
	Transfer Transfer
	Status   string
	Gates    []string // gates still required, in order
}

// Proposer records a proposed trust fee transfer. It only inserts the proposal row; it
// never posts to the trust ledger, the journals or AR.
type Proposer interface {
	Propose(ctx context.Context, p Proposal) (string, error)
}

// Approved is a bill approval as auto-apply sees it.
type Approved struct {
	BillID         string
	MatterID       string
	TrustAccountID string
	AutoApply      bool      // the auto_apply_trust_on_approval setting
	On             time.Time // business date of approval, America/Toronto
}

// AutoApplier proposes a trust fee transfer when a bill is approved and the
// auto_apply_trust_on_approval setting is on.
type AutoApplier struct {
	Applier  Applier
	Proposer Proposer
}

// OnApproved proposes min(bill balance - reserved, trust available) against the approved
// bill. It returns false when the setting is off or nothing is payable from trust. The
// transfer is checked like any proposal (BIL-C-050, BIL-C-051, BIL-C-054).
func (a AutoApplier) OnApproved(ctx context.Context, ev Approved) (Proposal, bool, error) {
	if !ev.AutoApply {
		return Proposal{}, false, nil
	}
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return Proposal{}, false, ErrDisabled
	}
	if ev.BillID == "" || ev.MatterID == "" || ev.TrustAccountID == "" {
		return Proposal{}, false, fmt.Errorf("auto-apply trust: %w", ErrInvalid)
	}
	b, err := a.Applier.Store.LockBill(ctx, ev.BillID)
	if err != nil {
		return Proposal{}, false, fmt.Errorf("lock bill %s: %w", ev.BillID, err)
	}
	funds, err := a.Applier.Store.LockFunds(ctx, ev.TrustAccountID, ev.MatterID)
	if err != nil {
		return Proposal{}, false, fmt.Errorf("lock trust ledger: %w", err)
	}
	avail := funds.Available()
	amount := min(b.BalanceCents-b.ReservedCents, avail)
	if amount <= 0 {
		return Proposal{}, false, nil
	}
	t := Transfer{BillID: ev.BillID, TrustAccountID: ev.TrustAccountID, MatterID: ev.MatterID, AmountCents: amount, On: ev.On}
	if _, err := a.Applier.Check(ctx, t); err != nil {
		return Proposal{}, false, fmt.Errorf("auto-apply trust: %w", err)
	}
	// rule: BIL-C-052
	// Auto-apply only creates the transfer in 'proposed'; G-B4 and G11 are still required
	// before any trust money moves.
	p := Proposal{Transfer: t, Status: StatusProposed, Gates: append([]string(nil), AutoApplyGates...)}
	id, err := a.Proposer.Propose(ctx, p)
	if err != nil {
		return Proposal{}, false, fmt.Errorf("propose trust fee transfer: %w", err)
	}
	p.ID = id
	return p, true, nil
}
