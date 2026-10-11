package credit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// Gate kinds a write-off moves through. Both carry spec id G-B5 (GateWriteOff) as a label.
const (
	// GateKindCreditNote is the G-B5 gate for a credit note or a write-off.
	GateKindCreditNote = "billing.credit_note"
	// GateKindWriteOffSecond is the managing lawyer's second approval of a write-off above
	// the firm threshold (spec/gates.json, approver billing.writeoff_second_approve).
	GateKindWriteOffSecond = "billing.writeoff_second_approval"
)

// SubjectWriteOff is the gate subject kind of a write-off; Subject.ID is the write-off id.
const SubjectWriteOff = "billing.write_off"

// payloadKindWriteOff is the G-B5 payload kind of a write-off.
const payloadKindWriteOff = "write_off"

// creditNotePayload is the billing.credit_note gate payload (spec/gates.json).
type creditNotePayload struct {
	BillID      string `json:"bill_id"`
	AmountCents int64  `json:"amount_cents"`
	Kind        string `json:"kind"`
	Reason      string `json:"reason"`
}

// SecondApprovalPayload is the billing.writeoff_second_approval gate payload.
type SecondApprovalPayload struct {
	WriteOffID    string `json:"write_off_id"`
	BillID        string `json:"bill_id"`
	AmountCents   int64  `json:"amount_cents"`
	FirstApprover string `json:"first_approver"`
}

// WriteOffBill is a bill as a write-off approval reads it: its open balance and the tax and
// total it was issued with.
type WriteOffBill struct {
	Bill
	TaxCents   int64 // bill tax as issued
	TotalCents int64 // bill total including tax, as issued
}

// WriteOffStore keeps write-offs. Every method runs in the caller's tx.
type WriteOffStore interface {
	// Lock reads write-off id FOR UPDATE, or returns ErrNotFound.
	Lock(ctx context.Context, tx pgx.Tx, id string) (WriteOff, error)
	// MarkAwaitingSecond records the pending second-approval gate; the write-off stays draft.
	MarkAwaitingSecond(ctx context.Context, tx pgx.Tx, id, gateID string) error
	// SaveApproved stores an approved write-off.
	SaveApproved(ctx context.Context, tx pgx.Tx, w WriteOff) error
	// AwaitingSecond returns the pending second-approval gate id MarkAwaitingSecond recorded
	// for write-off id, or "" when there is none.
	AwaitingSecond(ctx context.Context, tx pgx.Tx, id string) (string, error)
}

// WriteOffBills reads the bill a write-off reduces, in the caller's tx.
type WriteOffBills interface {
	WriteOffBill(ctx context.Context, tx pgx.Tx, id string) (WriteOffBill, error)
}

// BadDebtPoster posts the bad-debt tax adjustment of an approved write-off, in the caller's tx.
type BadDebtPoster interface {
	PostBadDebt(ctx context.Context, tx pgx.Tx, writeOffID string, adj BadDebtAdjustment) error
}

// GateOpener opens a gate in the caller's tx; gate.Engine and gatefake implement it.
type GateOpener interface {
	Open(ctx context.Context, tx pgx.Tx, kind string, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.GateID, error)
}

// WriteOffEffect applies an approved G-B5 gate for a write-off. At or below the firm
// threshold it approves the write-off and posts its bad-debt adjustment; above it, it posts
// nothing and opens the managing lawyer's second-approval gate, prepared by the G-B5
// approver. It fails closed: on any error nothing is applied.
type WriteOffEffect struct {
	Calc   Calc
	Config SecondApprovalConfig
	Store  WriteOffStore
	Bills  WriteOffBills
	Ledger BadDebtPoster
	Gates  GateOpener
	Clock  clock.Clock
}

// Effects returns the write-off effect handlers for gates.RegisterKinds: the G-B5
// billing.credit_note write-off effect and the managing lawyer's
// billing.writeoff_second_approval, both slugs from spec/gates.json. Every error a handler
// returns wraps gate.ErrEffectFailed, so nothing is applied and the gate stays undone.
func Effects(e *WriteOffEffect, second SecondApproval) map[string]registry.EffectFunc {
	s := &WriteOffSecondEffect{WriteOff: e, Second: second}
	return map[string]registry.EffectFunc{
		GateKindCreditNote:     effectFailed(e.Apply),
		GateKindWriteOffSecond: effectFailed(s.Apply),
	}
}

// effectFailed wraps every error of fn in gate.ErrEffectFailed.
func effectFailed(fn registry.EffectFunc) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		if err := fn(ctx, tx, g); err != nil {
			return fmt.Errorf("%w: %w", gate.ErrEffectFailed, err)
		}
		return nil
	}
}

// Apply is the EffectFunc of billing.credit_note for a write-off. A credit-note payload is
// not a write-off and is rejected.
func (e *WriteOffEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	var p creditNotePayload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.Kind != payloadKindWriteOff || g.Subject.ID == "" {
		return fmt.Errorf("write-off effect %s: payload: %w", g.GateID, ErrInvalidState)
	}
	if len(g.Approvers) == 0 {
		return fmt.Errorf("write-off effect %s: %w", g.GateID, ErrSelfApproval)
	}
	// The approver is the gate's first approver, never its preparer; approverOf makes an
	// agent or guest an agent, which ApproveWriteOff refuses.
	first := approverOf(g.Approvers[0])
	w, err := e.Store.Lock(ctx, tx, g.Subject.ID)
	if err != nil {
		return fmt.Errorf("write-off effect %s: %w", g.GateID, err)
	}
	if w.BillID != p.BillID || w.AmountCents != p.AmountCents {
		return fmt.Errorf("write-off effect %s: payload does not match write-off %s: %w", g.GateID, w.ID, ErrInvalidState)
	}
	// A replay of an applied effect changes nothing: the write-off is already approved (its
	// adjustment posted), or its second-approval gate is already open.
	if w.Status == StatusApproved {
		return nil
	}
	awaiting, err := e.Store.AwaitingSecond(ctx, tx, w.ID)
	if err != nil {
		return fmt.Errorf("write-off effect %s: %w", g.GateID, err)
	}
	if awaiting != "" {
		return nil
	}
	b, err := e.Bills.WriteOffBill(ctx, tx, w.BillID)
	if err != nil {
		return fmt.Errorf("write-off effect %s: bill: %w", g.GateID, err)
	}
	// ApproveWriteOff is pure: it runs every G-B5 check (draft, delivered, responsible
	// lawyer, not the preparer, not an agent, open balance) before anything is applied.
	approved, adj, err := e.Calc.ApproveWriteOff(w, b.Bill, b.TaxCents, b.TotalCents, first, businessDate(e.Clock))
	if err != nil {
		return fmt.Errorf("write-off effect %s: %w", g.GateID, err)
	}
	// rule: BIL-C-085
	// Above the threshold G-B5 alone posts nothing: the managing lawyer approves a second
	// time, and the G-B5 approver prepares that gate so they can not approve it again.
	if NeedsSecondApproval(w.AmountCents, e.Config) {
		payload, err := json.Marshal(SecondApprovalPayload{
			WriteOffID: w.ID, BillID: w.BillID, AmountCents: w.AmountCents, FirstApprover: first.UserID,
		})
		if err != nil {
			return fmt.Errorf("write-off effect %s: second payload: %w", g.GateID, err)
		}
		id, err := e.Gates.Open(ctx, tx, GateKindWriteOffSecond, gate.Subject{Kind: SubjectWriteOff, ID: w.ID}, payload,
			gate.Principal{Kind: gate.PrincipalUser, UserID: first.UserID})
		if err != nil {
			return fmt.Errorf("write-off effect %s: open second approval: %w", g.GateID, err)
		}
		if err := e.Store.MarkAwaitingSecond(ctx, tx, w.ID, string(id)); err != nil {
			return fmt.Errorf("write-off effect %s: %w", g.GateID, err)
		}
		return nil
	}
	return e.complete(ctx, tx, approved, adj)
}

// complete stores an approved write-off and posts its bad-debt adjustment.
func (e *WriteOffEffect) complete(ctx context.Context, tx pgx.Tx, w WriteOff, adj BadDebtAdjustment) error {
	if err := e.Store.SaveApproved(ctx, tx, w); err != nil {
		return fmt.Errorf("approve write-off %s: %w", w.ID, err)
	}
	// rule: BIL-C-205
	if err := e.Ledger.PostBadDebt(ctx, tx, w.ID, adj); err != nil {
		return fmt.Errorf("approve write-off %s: bad debt: %w", w.ID, err)
	}
	return nil
}

// approverOf maps a gate principal to an Approver. Only a user principal is a person; an
// agent or a guest counts as an agent and is never an approver.
func approverOf(p gate.Principal) Approver {
	if p.Kind == gate.PrincipalUser {
		return Approver{UserID: p.UserID}
	}
	return Approver{UserID: p.UserID, IsAgent: true}
}

// businessDate is today in the clock's business time zone (America/Toronto), at midnight.
func businessDate(c clock.Clock) time.Time {
	loc := c.Location()
	d := clock.DateOf(c.Now(), loc)
	return d.Start(loc)
}
