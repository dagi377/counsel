package bill

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateBillVoid is the gate (G-B2) whose effect is the only way an approved bill is voided.
const GateBillVoid = "billing.bill_void"

// VoidPayload is the billing.bill_void gate payload (spec/gates.json).
type VoidPayload struct {
	BillID string `json:"bill_id"`
	Reason string `json:"reason"`
}

// VoidStore reads and writes bills inside the void gate's tx.
type VoidStore interface {
	// Lock reads the bill FOR UPDATE, or returns ErrNotFound.
	Lock(ctx context.Context, tx pgx.Tx, id ID) (Bill, error)
	// SaveVoid moves an approved, undelivered bill to void. The number is kept.
	SaveVoid(ctx context.Context, tx pgx.Tx, b Bill) error
}

// ApprovalEntries finds the journal entry the approval of a bill posted (BIL-J-003).
type ApprovalEntries interface {
	ApprovalEntry(ctx context.Context, tx pgx.Tx, id ID) (ledger.EntryID, error)
}

// Reverser posts the exact reversal of a journal entry; ledger.Ledger satisfies it.
type Reverser interface {
	Reverse(ctx context.Context, tx pgx.Tx, id ledger.EntryID, reason string) (ledger.EntryID, error)
}

// VoidedSink is told about a void inside the gate's tx, after the bill is saved; an error
// rolls the void back.
type VoidedSink interface {
	Voided(ctx context.Context, tx pgx.Tx, plan VoidPlan, gateID string) error
}

// VoidEffect is the billing.bill_void effect: in the gate's tx it posts the exact reversal
// of the approval entry, keeps the bill's number and sets it to void. It never writes a
// credit note. Any error rolls the gate tx back.
type VoidEffect struct {
	Store   VoidStore
	Entries ApprovalEntries
	Ledger  Reverser
	Sink    VoidedSink       // optional
	Now     func() time.Time // the void instant; its Toronto date is the void date
}

// VoidEffects returns the effect handler for gates.RegisterKinds.
func VoidEffects(e *VoidEffect) map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{GateBillVoid: e.Apply}
}

// Apply is the billing.bill_void EffectFunc.
func (e *VoidEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	if g.Kind != GateBillVoid {
		return fmt.Errorf("bill void gate %s kind %q: %w", g.GateID, g.Kind, ErrInvalid)
	}
	var p VoidPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.BillID == "" || p.Reason == "" {
		return fmt.Errorf("bill void gate %s payload: %w", g.GateID, ErrInvalid)
	}
	if len(g.Approvers) == 0 {
		return fmt.Errorf("bill void gate %s: no approver: %w", g.GateID, ErrInvalid)
	}
	approver := g.Approvers[len(g.Approvers)-1]
	by := Voider{UserID: approver.UserID, IsAgent: approver.Kind != registry.PrincipalUser}
	requester := g.Preparer.UserID
	if g.Preparer.Kind == registry.PrincipalAgent {
		requester = g.Preparer.ActingFor
	}
	// rule: BIL-C-084
	// G-B2: whoever asked for the void does not decide it.
	if !by.IsAgent && by.UserID == requester {
		return fmt.Errorf("bill void gate %s: %w", g.GateID, ErrPreparerApproves)
	}
	b, err := e.Store.Lock(ctx, tx, ID(p.BillID))
	if err != nil {
		return fmt.Errorf("bill void gate %s: %w", g.GateID, err)
	}
	plan, err := planVoid(b, by, businessDate(e.Now()))
	if err != nil {
		return fmt.Errorf("bill void gate %s: %w", g.GateID, err)
	}
	if plan.Action != VoidReverse {
		// A bill that was never approved is discarded by its editor, not through G-B2.
		return fmt.Errorf("bill void gate %s: bill %s: %w", g.GateID, b.ID, ErrNotApproved)
	}
	entry, err := e.Entries.ApprovalEntry(ctx, tx, b.ID)
	if err != nil {
		return fmt.Errorf("bill void gate %s: approval entry: %w", g.GateID, err)
	}
	// rule: BIL-C-084
	// The exact reversal of the approval entry, dated the void date (BIL-J-012). The bill
	// keeps its number and no credit note is written.
	if _, err := e.Ledger.Reverse(ctx, tx, entry, "bill void "+g.GateID); err != nil {
		return fmt.Errorf("bill void gate %s: reverse: %w", g.GateID, err)
	}
	b.Status = plan.Status
	if err := e.Store.SaveVoid(ctx, tx, b); err != nil {
		return fmt.Errorf("bill void gate %s: %w", g.GateID, err)
	}
	if e.Sink != nil {
		if err := e.Sink.Voided(ctx, tx, plan, g.GateID); err != nil {
			return fmt.Errorf("bill void gate %s: %w", g.GateID, err)
		}
	}
	return nil
}

var _ VoidStore = PGStore{}

// SaveVoid moves an approved, undelivered bill to void and keeps its number. The database
// checks the approved billing.bill_void gate (bill_guard) and marks its lines voided so
// their entries can be billed again (bill_void_lines, CF-08).
func (PGStore) SaveVoid(ctx context.Context, tx pgx.Tx, b Bill) error {
	if b.Status != StatusVoid {
		return fmt.Errorf("save void of bill %s: %w", b.ID, ErrInvalid)
	}
	tag, err := tx.Exec(ctx, `
UPDATE billing.bill SET status = 'void'
 WHERE id = $1::uuid AND status = 'approved' AND delivered_at IS NULL`, string(b.ID))
	if err != nil {
		return fmt.Errorf("save void of bill %s: %w", b.ID, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("save void of bill %s: %w", b.ID, ErrNotApproved)
	}
	return nil
}
