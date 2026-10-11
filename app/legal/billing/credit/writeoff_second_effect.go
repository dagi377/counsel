package credit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// WriteOffSecondEffect applies an approved billing.writeoff_second_approval gate: the
// managing lawyer's second approval completes a write-off above the firm threshold. It
// approves the write-off for the G-B5 approver and posts its bad-debt adjustment once. A
// rejected second gate never runs it, so the write-off stays draft. It fails closed: on any
// error nothing is applied.
type WriteOffSecondEffect struct {
	// WriteOff supplies the calc, stores, ledger and clock the G-B5 effect uses.
	WriteOff *WriteOffEffect
	// Second decides who is a managing lawyer (the owner role).
	Second SecondApproval
}

// Apply is the EffectFunc of billing.writeoff_second_approval. Running it again for a
// write-off that is already approved is a no-op.
func (e *WriteOffSecondEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	var p SecondApprovalPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.WriteOffID == "" || p.FirstApprover == "" || g.Subject.ID != p.WriteOffID {
		return fmt.Errorf("second approval %s: payload: %w", g.GateID, ErrInvalidState)
	}
	// rule: BIL-C-085
	// The G-B5 approver prepared this gate; a gate prepared by anyone else is not a second
	// approval of that write-off.
	if g.Preparer.Kind != gate.PrincipalUser || g.Preparer.UserID != p.FirstApprover {
		return fmt.Errorf("second approval %s: preparer is not the first approver: %w", g.GateID, ErrInvalidState)
	}
	if len(g.Approvers) == 0 {
		return fmt.Errorf("second approval %s: %w", g.GateID, ErrSecondApprovalRequired)
	}
	first := Approver{UserID: p.FirstApprover}
	second := approverOf(g.Approvers[len(g.Approvers)-1])
	wo := e.WriteOff
	w, err := wo.Store.Lock(ctx, tx, p.WriteOffID)
	if err != nil {
		return fmt.Errorf("second approval %s: %w", g.GateID, err)
	}
	if w.Status == StatusApproved {
		return nil // replay: the adjustment was posted when the write-off was approved
	}
	if w.BillID != p.BillID || w.AmountCents != p.AmountCents {
		return fmt.Errorf("second approval %s: payload does not match write-off %s: %w", g.GateID, w.ID, ErrInvalidState)
	}
	// rule: BIL-C-085
	if err := e.Second.CheckSecondApprover(w, first, second); err != nil {
		return fmt.Errorf("second approval %s: %w", g.GateID, err)
	}
	b, err := wo.Bills.WriteOffBill(ctx, tx, w.BillID)
	if err != nil {
		return fmt.Errorf("second approval %s: bill: %w", g.GateID, err)
	}
	// rule: BIL-C-205
	// ApproveWriteOff re-runs the G-B5 checks on the first approver and gives the bad-debt
	// adjustment for the bill's open tax, in the period of the second approval.
	approved, adj, err := wo.Calc.ApproveWriteOff(w, b.Bill, b.TaxCents, b.TotalCents, first, businessDate(wo.Clock))
	if err != nil {
		return fmt.Errorf("second approval %s: %w", g.GateID, err)
	}
	return wo.complete(ctx, tx, approved, adj)
}
