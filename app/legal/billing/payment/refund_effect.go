package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateKindRefund is the G-B6 gate (spec/gates.json billing.refund). Its effect, never a
// route, records a refund (app/legal.GateOwned payment.refund).
const GateKindRefund = "billing.refund"

// RefundGLSource is the gl.journal_entry source_type of a refund entry; the source id is the
// billing.refund id, so a replayed posting returns the first entry.
const RefundGLSource = "refund"

// Where a payment was deposited (billing.payment.deposit_to).
const (
	DepositOperating = "operating"
	DepositTrust     = "trust"
)

// ErrRefundExceedsPayment is returned when a refund is larger than what is left of the payment.
var ErrRefundExceedsPayment = errors.New("payment: refund exceeds payment")

// Refundable is a payment as the refund effect reads it under lock.
type Refundable struct {
	PaymentID     string
	ClientID      string
	AmountCents   int64
	RefundedCents int64  // refunds already recorded against the payment
	DepositTo     string // DepositOperating or DepositTrust
	// TrustAccountID and MatterID name the client ledger a trust deposit went to.
	TrustAccountID string
	MatterID       string
}

// RefundRecord is one billing.refund row the effect writes.
type RefundRecord struct {
	PaymentID         string
	ClientID          string
	AmountCents       int64
	Reason            string
	ApprovalID        string // the billing.refund gate
	TrustWithdrawalID string // trust refunds only: the withdrawal that pays it out
	ApprovedBy        string
}

// RefundStore keeps refunds. Every method runs in the caller's tx.
type RefundStore interface {
	// LockRefundable reads payment id FOR UPDATE with its refunded total, or ErrNotFound.
	LockRefundable(ctx context.Context, tx pgx.Tx, id string) (Refundable, error)
	// RefundByApproval returns the refund id recorded under approvalID, or "" when none is.
	RefundByApproval(ctx context.Context, tx pgx.Tx, approvalID string) (string, error)
	// RecordRefund inserts the billing.refund row (status approved) and, for an operating
	// refund, the negative billing.client_credit row; it moves the payment to
	// partially_refunded or refunded. It returns the refund id.
	RecordRefund(ctx context.Context, tx pgx.Tx, r RefundRecord) (string, error)
}

// TrustRefunds is how a trust refund leaves trust: as a trust withdrawal, under the same
// rules and the same trust.withdrawal gate as any other.
type TrustRefunds interface {
	// Funds locks the client ledger and returns its position for withdraw.Validate.
	Funds(ctx context.Context, tx pgx.Tx, trustAccountID, matterID string) (withdraw.Funds, error)
	// Request records the validated withdrawal and opens its trust.withdrawal gate,
	// returning the withdrawal id. The same IdempotencyKey returns the first withdrawal.
	Request(ctx context.Context, tx pgx.Tx, r withdraw.Request) (string, error)
}

// RefundPayload is the billing.refund gate payload (spec/gates.json).
type RefundPayload struct {
	PaymentID   string `json:"payment_id"`
	AmountCents int64  `json:"amount_cents"`
	Payee       string `json:"payee"`
	Reason      string `json:"reason"`
}

// RefundEffect applies an approved billing.refund gate. An operating refund records the
// refund and posts Dr OP-2200 client credit / Cr OP-1000 operating bank. A trust refund never
// pays out here: it becomes a trust withdrawal (purpose client refund) checked by
// withdraw.Validate and authorised through trust.withdrawal, and the trust book moves when
// that withdrawal executes. It fails closed: on any error nothing is applied.
type RefundEffect struct {
	Store  RefundStore
	Trust  TrustRefunds
	Ledger ledger.Ledger
	Clock  clock.Clock
}

// Effects returns the billing.refund effect for gates.RegisterWired. Every error wraps
// gate.ErrEffectFailed.
func (e *RefundEffect) Effects() map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{
		GateKindRefund: func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
			if err := e.Apply(ctx, tx, g); err != nil {
				return fmt.Errorf("%w: %w", gate.ErrEffectFailed, err)
			}
			return nil
		},
	}
}

// Apply is the EffectFunc of billing.refund.
func (e *RefundEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	if e.Store == nil || e.Trust == nil || e.Ledger == nil || e.Clock == nil {
		return fmt.Errorf("refund effect %s: not configured: %w", g.GateID, ErrInvalid)
	}
	var p RefundPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.PaymentID == "" || p.PaymentID != g.Subject.ID ||
		p.AmountCents <= 0 || strings.TrimSpace(p.Payee) == "" || strings.TrimSpace(p.Reason) == "" {
		return fmt.Errorf("refund effect %s: payload: %w", g.GateID, ErrInvalid)
	}
	// rule: BIL-C-064
	// Approved by a person who is not the requester; the engine checks it and so does the
	// effect, so money never moves on an agent's or the requester's decision.
	if len(g.Approvers) == 0 {
		return fmt.Errorf("refund effect %s: no approver: %w", g.GateID, ErrPreparerApproves)
	}
	approver := g.Approvers[0]
	if approver.Kind != registry.PrincipalUser || approver.UserID == "" || approver.UserID == requesterOf(g.Preparer) {
		return fmt.Errorf("refund effect %s: approver: %w", g.GateID, ErrPreparerApproves)
	}
	pay, err := e.Store.LockRefundable(ctx, tx, p.PaymentID)
	if err != nil {
		return fmt.Errorf("refund effect %s: %w", g.GateID, err)
	}
	// A replay of an applied effect changes nothing.
	done, err := e.Store.RefundByApproval(ctx, tx, g.GateID)
	if err != nil {
		return fmt.Errorf("refund effect %s: %w", g.GateID, err)
	}
	if done != "" {
		return nil
	}
	// rule: BIL-C-064
	// Integer cents: the refund never exceeds what is left of the payment.
	if p.AmountCents > pay.AmountCents-pay.RefundedCents {
		return fmt.Errorf("refund effect %s: refund %d, left %d: %w", g.GateID, p.AmountCents,
			pay.AmountCents-pay.RefundedCents, ErrRefundExceedsPayment)
	}
	rec := RefundRecord{PaymentID: pay.PaymentID, ClientID: pay.ClientID, AmountCents: p.AmountCents,
		Reason: p.Reason, ApprovalID: g.GateID, ApprovedBy: approver.UserID}
	switch pay.DepositTo {
	case DepositTrust:
		return e.trust(ctx, tx, g, p, pay, rec)
	case DepositOperating:
		return e.operating(ctx, tx, g, rec)
	}
	return fmt.Errorf("refund effect %s: deposit %q: %w", g.GateID, pay.DepositTo, ErrInvalid)
}

// operating records the refund and posts it to the operating book.
func (e *RefundEffect) operating(ctx context.Context, tx pgx.Tx, g registry.Approved, rec RefundRecord) error {
	id, err := e.Store.RecordRefund(ctx, tx, rec)
	if err != nil {
		return fmt.Errorf("refund effect %s: %w", g.GateID, err)
	}
	amt := ledger.Cents(rec.AmountCents)
	subject := ledger.Subject{Kind: SubjectKind, ID: rec.PaymentID}
	entry := ledger.Entry{
		Book: books.BookOperating, Date: e.Clock.Today(ctx), SourceKind: RefundGLSource, SourceID: id,
		Memo: "refund", Lines: []ledger.Line{
			{Account: books.AcctClientCredit, Side: ledger.Debit, Amount: amt, Subject: subject},
			{Account: books.AcctOperatingBank, Side: ledger.Credit, Amount: amt, Subject: subject},
		},
	}
	if _, err := e.Ledger.Post(ctx, tx, entry); err != nil {
		return fmt.Errorf("refund effect %s: post: %w", g.GateID, err)
	}
	return nil
}

// trust turns the refund into a trust withdrawal under the trust withdrawal rules.
func (e *RefundEffect) trust(ctx context.Context, tx pgx.Tx, g registry.Approved, p RefundPayload, pay Refundable, rec RefundRecord) error {
	if pay.TrustAccountID == "" || pay.MatterID == "" {
		return fmt.Errorf("refund effect %s: trust deposit without ledger: %w", g.GateID, ErrInvalid)
	}
	// The G-B6 approver prepares the withdrawal, so they can not also authorise it.
	req := withdraw.Request{
		TrustAccountID: pay.TrustAccountID, MatterID: pay.MatterID, AmountCents: p.AmountCents, Payee: p.Payee,
		Method: withdraw.MethodCheque, Reason: "client refund: " + p.Reason, PreparedBy: rec.ApprovedBy,
		IdempotencyKey: "refund:" + g.GateID,
	}
	funds, err := e.Trust.Funds(ctx, tx, pay.TrustAccountID, pay.MatterID)
	if err != nil {
		return fmt.Errorf("refund effect %s: trust funds: %w", g.GateID, err)
	}
	// rule: BIL-L-037
	// The same checks as any trust withdrawal: no bearer payee, no overdraw, no uncleared
	// funds, and nothing while the By-law 9 rules are unverified.
	if err := withdraw.Validate(ctx, req, funds); err != nil {
		return fmt.Errorf("refund effect %s: %w", g.GateID, err)
	}
	wid, err := e.Trust.Request(ctx, tx, req)
	if err != nil {
		return fmt.Errorf("refund effect %s: trust withdrawal: %w", g.GateID, err)
	}
	rec.TrustWithdrawalID = wid
	if _, err := e.Store.RecordRefund(ctx, tx, rec); err != nil {
		return fmt.Errorf("refund effect %s: %w", g.GateID, err)
	}
	return nil
}

// requesterOf is the user who counts as the requester: an agent's acting-for user.
func requesterOf(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
}
