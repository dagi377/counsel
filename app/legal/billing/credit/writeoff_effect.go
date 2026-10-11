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
	TaxCents   int64  // bill tax as issued
	TotalCents int64  // bill total including tax, as issued
	ClientID   string // the client (payer) the bill is addressed to
}

// WriteOffClientWindow is how far back write-offs on the client's other bills count toward
// the second-approval threshold (BIL-C-085, verify).
const WriteOffClientWindow = 30 * 24 * time.Hour

// WriteOffScope names the write-offs that count toward one write-off's threshold.
type WriteOffScope struct {
	BillID    string    // every write-off on this bill
	ClientID  string    // and every write-off on this client's other bills ...
	Since     time.Time // ... created at or after Since
	ExcludeID string    // the write-off being decided, never counted twice
}

// WriteOffTotals sums earlier write-offs toward the second-approval threshold, in the
// caller's tx.
type WriteOffTotals interface {
	// CumulativeCents locks bill s.BillID FOR UPDATE, so two write-offs on one bill are
	// decided one after the other, then returns the cents of every write-off in s that is
	// approved, awaiting its second approval or in draft with a gate open. Each write-off
	// counts once, and s.ExcludeID never counts.
	CumulativeCents(ctx context.Context, tx pgx.Tx, s WriteOffScope) (int64, error)
}

// WriteOffStore keeps write-offs. Every method runs in the caller's tx.
type WriteOffStore interface {
	// Lock reads write-off id FOR UPDATE, or returns ErrNotFound.
	Lock(ctx context.Context, tx pgx.Tx, id string) (WriteOff, error)
	// MarkAwaitingSecond records the pending second-approval gate; the write-off stays draft.
	MarkAwaitingSecond(ctx context.Context, tx pgx.Tx, id, gateID string) error
	// SaveApproved stores an approved write-off.
	SaveApproved(ctx context.Context, tx pgx.Tx, w WriteOff) error
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
	// Totals sums earlier write-offs on the bill and the client. Nil fails closed.
	Totals WriteOffTotals
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
	first := approverOf(g.Approvers[len(g.Approvers)-1])
	w, err := e.Store.Lock(ctx, tx, g.Subject.ID)
	if err != nil {
		return fmt.Errorf("write-off effect %s: %w", g.GateID, err)
	}
	if w.BillID != p.BillID || w.AmountCents != p.AmountCents {
		return fmt.Errorf("write-off effect %s: payload does not match write-off %s: %w", g.GateID, w.ID, ErrInvalidState)
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
	// The threshold applies to the write-off plus every earlier one on the same bill and on
	// the client's bills in the last 30 days, so splitting one write-off never avoids it.
	cumulative, err := e.cumulativeCents(ctx, tx, w, b)
	if err != nil {
		return fmt.Errorf("write-off effect %s: %w", g.GateID, err)
	}
	// rule: BIL-C-085
	// Above the threshold G-B5 alone posts nothing: the managing lawyer approves a second
	// time, and the G-B5 approver prepares that gate so they can not approve it again.
	if NeedsSecondApproval(cumulative, e.Config) {
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

// cumulativeCents is w plus every earlier write-off that counts toward its threshold.
func (e *WriteOffEffect) cumulativeCents(ctx context.Context, tx pgx.Tx, w WriteOff, b WriteOffBill) (int64, error) {
	if e.Totals == nil {
		return 0, fmt.Errorf("write-off %s: no write-off totals: %w", w.ID, ErrInvalidState)
	}
	since := businessDate(e.Clock).Add(-WriteOffClientWindow)
	prior, err := e.Totals.CumulativeCents(ctx, tx, WriteOffScope{
		BillID: w.BillID, ClientID: b.ClientID, Since: since, ExcludeID: w.ID,
	})
	if err != nil {
		return 0, fmt.Errorf("write-off %s: totals: %w", w.ID, err)
	}
	if prior < 0 {
		return 0, fmt.Errorf("write-off %s: negative prior total: %w", w.ID, ErrInvalidState)
	}
	return prior + w.AmountCents, nil
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
