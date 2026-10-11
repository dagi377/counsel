package payment_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	refundPayment = "0192a000-0000-7000-8000-000000000001"
	refundClerk   = "clerk"
	refundLawyer  = "lawyer"
)

// memRefunds is an in-memory payment.RefundStore over one payment.
type memRefunds struct {
	pay     payment.Refundable
	records []payment.RefundRecord
}

func (m *memRefunds) LockRefundable(_ context.Context, _ pgx.Tx, id string) (payment.Refundable, error) {
	if id != m.pay.PaymentID {
		return payment.Refundable{}, payment.ErrNotFound
	}
	return m.pay, nil
}

func (m *memRefunds) RefundByApproval(_ context.Context, _ pgx.Tx, approvalID string) (string, error) {
	for i, r := range m.records {
		if r.ApprovalID == approvalID {
			return fmt.Sprintf("refund-%d", i+1), nil
		}
	}
	return "", nil
}

func (m *memRefunds) RecordRefund(_ context.Context, _ pgx.Tx, r payment.RefundRecord) (string, error) {
	m.records = append(m.records, r)
	m.pay.RefundedCents += r.AmountCents
	return fmt.Sprintf("refund-%d", len(m.records)), nil
}

// memTrust is an in-memory payment.TrustRefunds with fixed funds.
type memTrust struct {
	funds    withdraw.Funds
	requests []withdraw.Request
}

func (m *memTrust) Funds(context.Context, pgx.Tx, string, string) (withdraw.Funds, error) {
	return m.funds, nil
}

func (m *memTrust) Request(_ context.Context, _ pgx.Tx, r withdraw.Request) (string, error) {
	m.requests = append(m.requests, r)
	return fmt.Sprintf("wd-%d", len(m.requests)), nil
}

func refundApproved(t *testing.T, gateID string, amount int64, payee string, approvers ...registry.Principal) registry.Approved {
	t.Helper()
	raw, err := json.Marshal(payment.RefundPayload{PaymentID: refundPayment, AmountCents: amount, Payee: payee, Reason: "overpaid"})
	if err != nil {
		t.Fatal(err)
	}
	return registry.Approved{GateID: gateID, Kind: payment.GateKindRefund, Version: 1,
		Subject: registry.Subject{Kind: payment.SubjectKind, ID: refundPayment}, Payload: raw,
		Preparer: registry.Principal{Kind: registry.PrincipalUser, UserID: refundClerk}, Approvers: approvers}
}

// rule: BIL-C-064
// rule: BIL-L-037
func TestRefundEffect(t *testing.T) {
	lawyer := registry.Principal{Kind: registry.PrincipalUser, UserID: refundLawyer}
	clerk := registry.Principal{Kind: registry.PrincipalUser, UserID: refundClerk}
	agent := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: refundLawyer}
	operating := payment.Refundable{PaymentID: refundPayment, ClientID: "client-1", AmountCents: 10_000, DepositTo: payment.DepositOperating}
	trustPay := payment.Refundable{PaymentID: refundPayment, ClientID: "client-1", AmountCents: 10_000, DepositTo: payment.DepositTrust,
		TrustAccountID: "acct-1", MatterID: "matter-1"}
	rich := withdraw.Funds{ClearedCents: 50_000}
	for _, c := range []struct {
		name        string
		pay         payment.Refundable
		funds       withdraw.Funds
		flagOn      bool
		amount      int64
		payee       string
		approvers   []registry.Principal
		runs        int
		wantErr     error
		wantRecords int
		wantGL      int64 // cents credited to OP-1000
		wantWD      int
	}{
		{name: "operating refund records payment and journal", pay: operating, amount: 2_501, payee: "A. Client",
			approvers: []registry.Principal{lawyer}, runs: 1, wantRecords: 1, wantGL: 2_501},
		{name: "replay records once", pay: operating, amount: 2_501, payee: "A. Client",
			approvers: []registry.Principal{lawyer}, runs: 2, wantRecords: 1, wantGL: 2_501},
		{name: "full refund", pay: operating, amount: 10_000, payee: "A. Client",
			approvers: []registry.Principal{lawyer}, runs: 1, wantRecords: 1, wantGL: 10_000},
		{name: "exceeds payment", pay: operating, amount: 10_001, payee: "A. Client",
			approvers: []registry.Principal{lawyer}, runs: 1, wantErr: payment.ErrRefundExceedsPayment},
		{name: "exceeds what is left", pay: payment.Refundable{PaymentID: refundPayment, AmountCents: 10_000, RefundedCents: 9_000,
			DepositTo: payment.DepositOperating}, amount: 1_001, payee: "A. Client",
			approvers: []registry.Principal{lawyer}, runs: 1, wantErr: payment.ErrRefundExceedsPayment},
		{name: "requester cannot approve", pay: operating, amount: 100, payee: "A. Client",
			approvers: []registry.Principal{clerk}, runs: 1, wantErr: payment.ErrPreparerApproves},
		{name: "agent cannot approve", pay: operating, amount: 100, payee: "A. Client",
			approvers: []registry.Principal{agent}, runs: 1, wantErr: payment.ErrPreparerApproves},
		{name: "no approver", pay: operating, amount: 100, payee: "A. Client",
			runs: 1, wantErr: payment.ErrPreparerApproves},
		{name: "trust refund becomes a withdrawal", pay: trustPay, funds: rich, flagOn: true, amount: 4_000, payee: "A. Client",
			approvers: []registry.Principal{lawyer}, runs: 1, wantRecords: 1, wantWD: 1},
		{name: "trust refund over available", pay: trustPay, funds: withdraw.Funds{ClearedCents: 3_999}, flagOn: true, amount: 4_000,
			payee: "A. Client", approvers: []registry.Principal{lawyer}, runs: 1, wantErr: withdraw.ErrTrustOverdraw},
		{name: "trust refund on uncleared funds", pay: trustPay, funds: withdraw.Funds{ClearedCents: 1_000, UnclearedCents: 5_000},
			flagOn: true, amount: 4_000, payee: "A. Client", approvers: []registry.Principal{lawyer}, runs: 1, wantErr: withdraw.ErrUnclearedFunds},
		{name: "trust refund to bearer", pay: trustPay, funds: rich, flagOn: true, amount: 4_000, payee: "cash",
			approvers: []registry.Principal{lawyer}, runs: 1, wantErr: withdraw.ErrBearerPayee},
		{name: "trust refund while by-law 9 unverified", pay: trustPay, funds: rich, amount: 4_000, payee: "A. Client",
			approvers: []registry.Principal{lawyer}, runs: 1, wantErr: withdraw.ErrUnverified},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := legalflagstest.Context(t, withdraw.FlagBylaw9Verified, c.flagOn)
			clk := clockfake.New(time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC), mustToronto(t))
			gl := ledgerfake.New(clk.Today(ctx), books.AcctClientCredit, books.AcctOperatingBank)
			store := &memRefunds{pay: c.pay}
			tr := &memTrust{funds: c.funds}
			e := &payment.RefundEffect{Store: store, Trust: tr, Ledger: gl, Clock: clk}
			fx := e.Effects()[payment.GateKindRefund]
			var err error
			for range c.runs {
				err = fx(ctx, nil, refundApproved(t, "gate-1", c.amount, c.payee, c.approvers...))
			}
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) || !errors.Is(err, gate.ErrEffectFailed) {
					t.Fatalf("effect = %v, want %v wrapped in ErrEffectFailed", err, c.wantErr)
				}
			} else if err != nil {
				t.Fatalf("effect = %v", err)
			}
			if len(store.records) != c.wantRecords {
				t.Fatalf("refund records = %d, want %d", len(store.records), c.wantRecords)
			}
			bank, err := gl.Balance(ctx, books.AcctOperatingBank, clk.Today(ctx))
			if err != nil {
				t.Fatal(err)
			}
			if -int64(bank) != c.wantGL {
				t.Errorf("OP-1000 credited %d, want %d", -int64(bank), c.wantGL)
			}
			if credit, _ := gl.Balance(ctx, books.AcctClientCredit, clk.Today(ctx)); int64(credit) != c.wantGL {
				t.Errorf("OP-2200 debited %d, want %d", int64(credit), c.wantGL)
			}
			if len(tr.requests) != c.wantWD {
				t.Fatalf("trust withdrawals = %d, want %d", len(tr.requests), c.wantWD)
			}
			if c.wantRecords == 1 {
				r := store.records[0]
				if r.AmountCents != c.amount || r.ApprovalID != "gate-1" || r.ApprovedBy != refundLawyer {
					t.Errorf("record = %+v", r)
				}
			}
			if c.wantWD == 1 {
				w := tr.requests[0]
				if w.AmountCents != c.amount || w.PreparedBy != refundLawyer || w.Method != withdraw.MethodCheque ||
					w.Payee != c.payee || store.records[0].TrustWithdrawalID != "wd-1" {
					t.Errorf("withdrawal = %+v, record = %+v", w, store.records[0])
				}
			}
		})
	}
}

func mustToronto(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

var _ ledger.Ledger = (*ledgerfake.Fake)(nil)
