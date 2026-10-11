package credit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/registry"
)

// payloadKindCreditNote is the G-B5 payload kind of a credit note.
const payloadKindCreditNote = "credit_note"

// SeriesCreditNote is the core.number_series series credit note numbers come from.
const SeriesCreditNote = "credit_note"

// SourceCreditNote is the gl source kind of a credit note's journal entry.
const SourceCreditNote = "credit_note"

// Credit note statuses as billing.credit_note stores them.
const (
	noteStatusDraft   = "draft"
	noteStatusPending = "pending_approval"
	noteStatusIssued  = "issued"
)

// StoredNote is one billing.credit_note row. AmountCents is pre-tax; TaxCents is the tax
// the note reverses, fixed when it was drafted from the source bill's tax lines.
type StoredNote struct {
	ID          string
	Number      string
	BillID      string
	Kind        Kind
	AmountCents int64
	TaxCents    int64
	Reason      string
	Status      string
	ApprovalID  string
	IssuedAt    *time.Time
}

// NoteStore keeps credit notes. Every method runs in the caller's tx.
type NoteStore interface {
	// Lock reads credit note id FOR UPDATE, or returns ErrNotFound.
	Lock(ctx context.Context, tx pgx.Tx, id string) (StoredNote, error)
	// Bucket returns the pre-tax billed on bill billID and what issued credit notes and
	// approved write-offs already took off it; credit note excludeID never counts.
	Bucket(ctx context.Context, tx pgx.Tx, billID, excludeID string) (Bucket, error)
	// Issue marks credit note id issued with its number and approval.
	Issue(ctx context.Context, tx pgx.Tx, id, number, approvalID string, at time.Time) error
}

// Numbers hands out the next gapless number of a series in the caller's tx;
// kernel/numseq implements it.
type Numbers interface {
	Next(ctx context.Context, tx pgx.Tx, series string) (string, error)
}

// NoteEffect applies an approved G-B5 gate for a credit note: it re-checks the bill and the
// credit limit under the note's lock, issues the note with its number and posts the credit
// through the ledger. It fails closed: on any error nothing is applied.
type NoteEffect struct {
	Store   NoteStore
	Bills   Bills
	Numbers Numbers
	Ledger  ledger.Ledger
	Clock   clock.Clock
}

// NoteEffects returns the credit-note effect handler for gates.RegisterKinds. Every error it
// returns wraps gate.ErrEffectFailed. Boot uses MergeEffects to join it with Effects, which
// handles the same slug for write-offs.
func NoteEffects(e *NoteEffect) map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{GateKindCreditNote: effectFailed(e.Apply)}
}

// MergeEffects joins NoteEffects and Effects into one map. billing.credit_note carries both
// credit notes and write-offs (spec/gates.json payload kind), so its handler dispatches on
// the payload kind; every other slug comes from exactly one source.
func MergeEffects(notes, writeOffs map[string]registry.EffectFunc) (map[string]registry.EffectFunc, error) {
	out := make(map[string]registry.EffectFunc, len(notes)+len(writeOffs))
	for k, fn := range writeOffs {
		out[k] = fn
	}
	for k, fn := range notes {
		if k == GateKindCreditNote {
			continue
		}
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("merge credit effects: slug %s: %w", k, ErrInvalidState)
		}
		out[k] = fn
	}
	note, wo := notes[GateKindCreditNote], writeOffs[GateKindCreditNote]
	if note == nil || wo == nil {
		return nil, fmt.Errorf("merge credit effects: %s needs both handlers: %w", GateKindCreditNote, ErrInvalidState)
	}
	out[GateKindCreditNote] = func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		var p creditNotePayload
		if err := json.Unmarshal(g.Payload, &p); err != nil {
			return effectFailed(func(context.Context, pgx.Tx, registry.Approved) error {
				return fmt.Errorf("credit effect %s: payload: %w", g.GateID, ErrInvalidState)
			})(ctx, tx, g)
		}
		if p.Kind == payloadKindWriteOff {
			return wo(ctx, tx, g)
		}
		return note(ctx, tx, g)
	}
	return out, nil
}

// Apply is the EffectFunc of billing.credit_note for a credit note. A write-off payload is
// not a credit note and is rejected.
func (e *NoteEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	var p creditNotePayload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.Kind != payloadKindCreditNote || g.Subject.ID == "" {
		return fmt.Errorf("credit note effect %s: payload: %w", g.GateID, ErrInvalidState)
	}
	if len(g.Approvers) == 0 {
		return fmt.Errorf("credit note effect %s: %w", g.GateID, ErrSelfApproval)
	}
	// The approver is the gate's first approver. An agent or guest never approves, and the
	// preparer (or the user an agent prepared for) never approves their own note.
	by := approverOf(g.Approvers[0])
	if by.IsAgent {
		return fmt.Errorf("credit note effect %s: %w", g.GateID, ErrAgentApproval)
	}
	if by.UserID == "" || by.UserID == preparerOf(g.Preparer) {
		return fmt.Errorf("credit note effect %s: %w", g.GateID, ErrSelfApproval)
	}
	n, err := e.Store.Lock(ctx, tx, g.Subject.ID)
	if err != nil {
		return fmt.Errorf("credit note effect %s: %w", g.GateID, err)
	}
	if n.BillID != p.BillID || n.AmountCents != p.AmountCents {
		return fmt.Errorf("credit note effect %s: payload does not match note %s: %w", g.GateID, n.ID, ErrInvalidState)
	}
	// A replay of the same gate changes nothing: the note is already issued under it.
	if n.Status == noteStatusIssued && n.ApprovalID == g.GateID {
		return nil
	}
	if n.Status != noteStatusDraft && n.Status != noteStatusPending {
		return fmt.Errorf("credit note effect %s: note %s is %s: %w", g.GateID, n.ID, n.Status, ErrInvalidState)
	}
	if n.AmountCents <= 0 || n.TaxCents < 0 {
		return fmt.Errorf("credit note effect %s: %w", g.GateID, ErrInvalidAmount)
	}
	b, err := e.Bills.Bill(ctx, n.BillID)
	if err != nil {
		return fmt.Errorf("credit note effect %s: bill: %w", g.GateID, err)
	}
	// rule: BIL-T-071
	if err := CheckDelivered(b); err != nil {
		return fmt.Errorf("credit note effect %s: %w", g.GateID, err)
	}
	bucket, err := e.Store.Bucket(ctx, tx, n.BillID, n.ID)
	if err != nil {
		return fmt.Errorf("credit note effect %s: bucket: %w", g.GateID, err)
	}
	// rule: BIL-C-082
	if err := CheckBucket(bucket, n.AmountCents); err != nil {
		return fmt.Errorf("credit note effect %s: %w", g.GateID, err)
	}
	number, err := e.Numbers.Next(ctx, tx, SeriesCreditNote)
	if err != nil {
		return fmt.Errorf("credit note effect %s: number: %w", g.GateID, err)
	}
	now := e.Clock.Now()
	if err := e.Store.Issue(ctx, tx, n.ID, number, g.GateID, now); err != nil {
		return fmt.Errorf("credit note effect %s: %w", g.GateID, err)
	}
	// rule: BIL-T-070
	// The reduction is reported on the credit note date, not the bill date.
	if _, err := e.Ledger.Post(ctx, tx, noteEntry(n, clock.DateOf(now, e.Clock.Location()))); err != nil {
		return fmt.Errorf("credit note effect %s: post: %w", g.GateID, err)
	}
	return nil
}

// noteEntry is the journal of an issued credit note: the pre-tax credit reduces fee revenue
// through fee adjustments, the tax reverses sales tax payable, and the total comes off AR.
func noteEntry(n StoredNote, date clock.Date) ledger.Entry {
	subject := ledger.Subject{Kind: "billing.credit_note", ID: n.ID}
	lines := []ledger.Line{{Account: books.AcctFeeAdjustments, Side: ledger.Debit, Amount: ledger.Cents(n.AmountCents), Subject: subject}}
	if n.TaxCents > 0 {
		lines = append(lines, ledger.Line{Account: books.AcctSalesTaxPayable, Side: ledger.Debit, Amount: ledger.Cents(n.TaxCents), Subject: subject})
	}
	lines = append(lines, ledger.Line{Account: books.AcctARFees, Side: ledger.Credit, Amount: ledger.Cents(n.AmountCents + n.TaxCents), Subject: subject})
	return ledger.Entry{
		Book: books.BookOperating, Date: date, SourceKind: SourceCreditNote, SourceID: n.ID,
		Memo: "credit note " + n.ID, Lines: lines,
	}
}

// preparerOf is the user a gate was prepared by or for: an agent's acting-for user counts
// as the preparer.
func preparerOf(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
}

// PGNotes is the Postgres NoteStore over billing.credit_note.
type PGNotes struct{}

var _ NoteStore = PGNotes{}

// Lock reads credit note id FOR UPDATE.
func (PGNotes) Lock(ctx context.Context, tx pgx.Tx, id string) (StoredNote, error) {
	var n StoredNote
	var number, approval *string
	err := tx.QueryRow(ctx, `SELECT id::text, number, bill_id::text, kind, amount_cents, tax_cents, reason, status,
       approval_id::text, issued_at
  FROM billing.credit_note WHERE id = $1::uuid FOR UPDATE`, id).
		Scan(&n.ID, &number, &n.BillID, &n.Kind, &n.AmountCents, &n.TaxCents, &n.Reason, &n.Status, &approval, &n.IssuedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredNote{}, fmt.Errorf("lock credit note %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return StoredNote{}, fmt.Errorf("lock credit note %s: %w", id, err)
	}
	if number != nil {
		n.Number = *number
	}
	if approval != nil {
		n.ApprovalID = *approval
	}
	return n, nil
}

// Bucket reads the bill's pre-tax total and what issued notes and approved write-offs
// already took off it. A billing.write_off row exists only once approved; its pre-tax share
// is fees plus disbursements.
func (PGNotes) Bucket(ctx context.Context, tx pgx.Tx, billID, excludeID string) (Bucket, error) {
	b := Bucket{Key: "bill"}
	err := tx.QueryRow(ctx, `SELECT fees_cents + taxable_disb_cents + nontaxable_disb_cents - discount_cents
  FROM billing.bill WHERE id = $1::uuid`, billID).Scan(&b.BilledCents)
	if errors.Is(err, pgx.ErrNoRows) {
		return Bucket{}, fmt.Errorf("credit bucket bill %s: %w", billID, ErrNotFound)
	}
	if err != nil {
		return Bucket{}, fmt.Errorf("credit bucket bill %s: %w", billID, err)
	}
	err = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount_cents), 0)::bigint FROM billing.credit_note
 WHERE bill_id = $1::uuid AND id <> $2::uuid AND status IN ('issued','refund_pending','closed')`, billID, excludeID).
		Scan(&b.CreditedCents)
	if err != nil {
		return Bucket{}, fmt.Errorf("credit bucket bill %s: credited: %w", billID, err)
	}
	err = tx.QueryRow(ctx, `SELECT COALESCE(sum(fee_cents + dtx_cents + dnt_cents), 0)::bigint FROM billing.write_off
 WHERE bill_id = $1::uuid`, billID).Scan(&b.WrittenOff)
	if err != nil {
		return Bucket{}, fmt.Errorf("credit bucket bill %s: written off: %w", billID, err)
	}
	return b, nil
}

// Issue marks credit note id issued. The row's CHECK requires number, issued_at and
// approval_id together.
func (PGNotes) Issue(ctx context.Context, tx pgx.Tx, id, number, approvalID string, at time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE billing.credit_note
   SET status = 'issued', number = $2, approval_id = $3::uuid, issued_at = $4
 WHERE id = $1::uuid AND status IN ('draft','pending_approval')`, id, number, approvalID, at)
	if err != nil {
		return fmt.Errorf("issue credit note %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("issue credit note %s: %w", id, ErrInvalidState)
	}
	return nil
}
