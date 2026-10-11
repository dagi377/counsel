package trustfake

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/trust"
)

var day = time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)

type opCash int64

func (o opCash) OperatingCashCents(context.Context, string) (int64, error) { return int64(o), nil }

// setup opens an account with signatory "sig" and a ledger for matter "m1".
func setup(t *testing.T, f *Fake, signatory bool) trust.LedgerID {
	t.Helper()
	ctx := context.Background()
	acct, err := f.OpenAccount(ctx, nil, trust.Account{Type: trust.AccountMixed, Title: "Mixed", OpenedOn: day})
	if err != nil {
		t.Fatal(err)
	}
	if signatory {
		if err := f.AddSignatory(ctx, nil, acct, "sig"); err != nil {
			t.Fatal(err)
		}
	}
	l, err := f.OpenLedger(ctx, nil, trust.Ledger{AccountID: acct, MatterID: "m1", ClientID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func receipt(l trust.LedgerID, amount int64, m trust.Method) trust.Receipt {
	r := trust.Receipt{LedgerID: l, TxnDate: day, Method: m, Counterparty: "client", AmountCents: amount, Purpose: "retainer", PostedBy: "u1"}
	if m != trust.MethodCheque {
		r.ClearedOn = day
	}
	return r
}

func TestReceive(t *testing.T) {
	tests := []struct {
		name      string
		signatory bool
		limits    Limits
		opCash    int64
		prior     int64
		r         func(l trust.LedgerID) trust.Receipt
		want      error
	}{
		{name: "receipt posts", signatory: true, r: func(l trust.LedgerID) trust.Receipt { return receipt(l, 150000, trust.MethodEFT) }},
		{name: "no active signatory", r: func(l trust.LedgerID) trust.Receipt { return receipt(l, 150000, trust.MethodEFT) }, want: trust.ErrNoSignatory},
		{name: "zero amount", signatory: true, r: func(l trust.LedgerID) trust.Receipt { return receipt(l, 0, trust.MethodEFT) }, want: trust.ErrInvalid},
		{name: "uncleared eft", signatory: true, r: func(l trust.LedgerID) trust.Receipt {
			r := receipt(l, 100, trust.MethodEFT)
			r.ClearedOn = time.Time{}
			return r
		}, want: trust.ErrInvalid},
		{name: "uncleared cheque ok", signatory: true, r: func(l trust.LedgerID) trust.Receipt { return receipt(l, 100, trust.MethodCheque) }},
		{name: "cash limit off by default", signatory: true, prior: 500000, r: func(l trust.LedgerID) trust.Receipt { return receipt(l, 250000, trust.MethodCash) }},
		{name: "cash under limit", signatory: true, limits: Limits{CashLimitCents: 750000, CashWarnCents: 600000}, prior: 500000,
			r: func(l trust.LedgerID) trust.Receipt { return receipt(l, 249999, trust.MethodCash) }},
		{name: "cash at limit", signatory: true, limits: Limits{CashLimitCents: 750000, CashWarnCents: 600000}, prior: 500000,
			r: func(l trust.LedgerID) trust.Receipt { return receipt(l, 250000, trust.MethodCash) }, want: trust.ErrCashLimit},
		{name: "operating cash counts", signatory: true, limits: Limits{CashLimitCents: 750000}, opCash: 500000,
			r: func(l trust.LedgerID) trust.Receipt { return receipt(l, 250000, trust.MethodCash) }, want: trust.ErrCashLimit},
		{name: "cash at limit with exception but no G6c gate", signatory: true, limits: Limits{CashLimitCents: 750000}, prior: 500000,
			r: func(l trust.LedgerID) trust.Receipt {
				r := receipt(l, 250000, trust.MethodCash)
				r.CashExceptionCode = "ex-1"
				return r
			}, want: trust.ErrCashGateRequired},
		{name: "cash at limit with exception and G6c gate", signatory: true, limits: Limits{CashLimitCents: 750000}, prior: 500000,
			r: func(l trust.LedgerID) trust.Receipt {
				r := receipt(l, 250000, trust.MethodCash)
				r.CashExceptionCode = "ex-1"
				r.CashGateID = "g6c-1"
				return r
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(tt.limits, opCash(tt.opCash))
			l := setup(t, f, tt.signatory)
			ctx := context.Background()
			if tt.prior > 0 {
				if _, err := f.Receive(ctx, nil, receipt(l, tt.prior, trust.MethodCash)); err != nil {
					t.Fatalf("prior receipt: %v", err)
				}
			}
			before, _ := f.Balance(ctx, l)
			r := tt.r(l)
			_, err := f.Receive(ctx, nil, r)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Receive err = %v, want %v", err, tt.want)
			}
			after, _ := f.Balance(ctx, l)
			want := before.PostedCents
			if err == nil {
				want += r.AmountCents
			}
			if after.PostedCents != want {
				t.Fatalf("posted = %d, want %d", after.PostedCents, want)
			}
		})
	}
}

func TestCheckCash(t *testing.T) {
	tests := []struct {
		name                string
		amount              int64
		wantWarn, wantBlock bool
	}{
		{name: "below warning", amount: 599999},
		{name: "warning", amount: 600000, wantWarn: true},
		{name: "limit", amount: 750000, wantWarn: true, wantBlock: true},
	}
	f := New(Limits{CashLimitCents: 750000, CashWarnCents: 600000}, nil)
	setup(t, f, true)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := f.CheckCash(context.Background(), "m1", tt.amount)
			if err != nil || c.Warn != tt.wantWarn || c.Blocked != tt.wantBlock || c.TotalCents != tt.amount {
				t.Fatalf("CheckCash = %+v, %v", c, err)
			}
		})
	}
}

func TestDisburse(t *testing.T) {
	tests := []struct {
		name      string
		protected int64
		method    trust.Method
		cheque    bool
		amount    int64
		want      error
		wantAvail int64
	}{
		{name: "within balance", amount: 4000, method: trust.MethodEFT, wantAvail: 6000},
		{name: "whole balance", amount: 10000, method: trust.MethodEFT, wantAvail: 0},
		{name: "over balance", amount: 12000, method: trust.MethodEFT, want: trust.ErrInsufficientFunds, wantAvail: 10000},
		{name: "protected funds held back", protected: 3000, amount: 8000, method: trust.MethodEFT, want: trust.ErrInsufficientFunds, wantAvail: 7000},
		{name: "uncleared cheque", cheque: true, amount: 4000, method: trust.MethodEFT, want: trust.ErrUnclearedFunds, wantAvail: 0},
		{name: "never in cash", amount: 100, method: trust.MethodCash, want: trust.ErrCashPayout, wantAvail: 10000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New(Limits{}, nil)
			acct, _ := f.OpenAccount(ctx, nil, trust.Account{Type: trust.AccountMixed, Title: "Mixed"})
			_ = f.AddSignatory(ctx, nil, acct, "sig")
			l, err := f.OpenLedger(ctx, nil, trust.Ledger{AccountID: acct, MatterID: "m1", ClientID: "c1", ProtectedCents: tt.protected, ProtectedPurpose: "hold"})
			if err != nil {
				t.Fatal(err)
			}
			m := trust.MethodEFT
			if tt.cheque {
				m = trust.MethodCheque
			}
			if _, err := f.Receive(ctx, nil, receipt(l, 10000, m)); err != nil {
				t.Fatal(err)
			}
			_, err = f.Disburse(ctx, nil, trust.Disbursement{LedgerID: l, WithdrawalID: "w1", TxnDate: day, Method: tt.method, Counterparty: "payee", AmountCents: tt.amount, Purpose: "p", PostedBy: "u2"})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Disburse err = %v, want %v", err, tt.want)
			}
			if b, _ := f.Balance(ctx, l); b.AvailableCents != tt.wantAvail {
				t.Fatalf("available = %d, want %d", b.AvailableCents, tt.wantAvail)
			}
		})
	}
}

func TestDisburseConcurrent(t *testing.T) {
	ctx := context.Background()
	f := New(Limits{}, nil)
	l := setup(t, f, true)
	if _, err := f.Receive(ctx, nil, receipt(l, 100000, trust.MethodEFT)); err != nil {
		t.Fatal(err)
	}
	const n, amount = 32, 7000
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.Disburse(ctx, nil, trust.Disbursement{LedgerID: l, WithdrawalID: "w", TxnDate: day, Method: trust.MethodEFT, Counterparty: "p", AmountCents: amount, Purpose: "p", PostedBy: "u"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	ok := 0
	for err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, trust.ErrInsufficientFunds):
			t.Fatalf("unexpected error %v", err)
		}
	}
	if ok*amount > 100000 || ok != 100000/amount {
		t.Fatalf("committed %d disbursements of %d from 100000", ok, amount)
	}
}

func TestReverseNSF(t *testing.T) {
	tests := []struct {
		name          string
		paidOut       int64
		approval      string
		want          error
		wantShortfall int64
		wantException bool
	}{
		{name: "ledger still covers it", paidOut: 0, wantShortfall: 0},
		{name: "already paid out", paidOut: 6000, approval: "g-1", wantShortfall: 6000, wantException: true},
		{name: "replenishment needs approval", paidOut: 6000, want: trust.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New(Limits{}, nil)
			l := setup(t, f, true)
			rid, _ := f.Receive(ctx, nil, receipt(l, 10000, trust.MethodEFT))
			if tt.paidOut > 0 {
				if _, err := f.Disburse(ctx, nil, trust.Disbursement{LedgerID: l, WithdrawalID: "w", TxnDate: day, Method: trust.MethodEFT, Counterparty: "p", AmountCents: tt.paidOut, Purpose: "p", PostedBy: "u"}); err != nil {
					t.Fatal(err)
				}
			}
			res, err := f.ReverseNSF(ctx, nil, rid, tt.approval, "u3")
			if !errors.Is(err, tt.want) {
				t.Fatalf("ReverseNSF err = %v, want %v", err, tt.want)
			}
			if err != nil {
				return
			}
			b, _ := f.Balance(ctx, l)
			if b.PostedCents != 0 && tt.paidOut > 0 || res.ShortfallCents != tt.wantShortfall || res.ExceptionOpened != tt.wantException {
				t.Fatalf("result %+v balance %+v", res, b)
			}
			if _, err := f.ReverseNSF(ctx, nil, rid, tt.approval, "u3"); !errors.Is(err, trust.ErrAlreadyReversed) {
				t.Fatalf("second reversal err = %v", err)
			}
		})
	}
}

func TestAccountsAndLedgers(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		run  func(f *Fake) error
		want error
	}{
		{name: "separate account needs matter", run: func(f *Fake) error {
			_, err := f.OpenAccount(ctx, nil, trust.Account{Type: trust.AccountSeparateInterestBearing, Title: "S"})
			return err
		}, want: trust.ErrInvalid},
		{name: "duplicate ledger", run: func(f *Fake) error {
			a, _ := f.OpenAccount(ctx, nil, trust.Account{Type: trust.AccountMixed, Title: "M"})
			_, _ = f.OpenLedger(ctx, nil, trust.Ledger{AccountID: a, MatterID: "m", ClientID: "c"})
			_, err := f.OpenLedger(ctx, nil, trust.Ledger{AccountID: a, MatterID: "m", ClientID: "c"})
			return err
		}, want: trust.ErrDuplicateLedger},
		{name: "protected needs purpose", run: func(f *Fake) error {
			a, _ := f.OpenAccount(ctx, nil, trust.Account{Type: trust.AccountMixed, Title: "M"})
			_, err := f.OpenLedger(ctx, nil, trust.Ledger{AccountID: a, MatterID: "m", ClientID: "c", ProtectedCents: 1})
			return err
		}, want: trust.ErrInvalid},
		{name: "removed signatory blocks receipts", run: func(f *Fake) error {
			l := setup(t, f, true)
			a, _ := f.Account(ctx, f.ledgers[l].AccountID)
			_ = f.RemoveSignatory(ctx, nil, a.ID, "sig")
			_, err := f.Receive(ctx, nil, receipt(l, 1, trust.MethodEFT))
			return err
		}, want: trust.ErrNoSignatory},
		{name: "unknown account", run: func(f *Fake) error { _, err := f.Account(ctx, "x"); return err }, want: trust.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(New(Limits{}, nil)); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
