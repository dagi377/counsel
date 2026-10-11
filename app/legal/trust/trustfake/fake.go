// Package trustfake is an in-memory trust.Service for tests: no network, Postgres or
// Docker. One mutex stands in for the ledger row lock.
package trustfake

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/jackc/pgx/v5"
)

var _ trust.Service = (*Fake)(nil)

// Limits sets the cash limit. Zero values switch the check off, which is the default
// until the cash limit rule is confirmed.
type Limits struct {
	CashLimitCents int64
	CashWarnCents  int64
}

// Fake is an in-memory trust service.
type Fake struct {
	mu         sync.Mutex
	limits     Limits
	cash       trust.CashSource
	accounts   map[trust.AccountID]trust.Account
	ledgers    map[trust.LedgerID]trust.Ledger
	txns       []trust.Txn
	exceptions int
	seq        int
}

// New returns an empty service. src may be nil (no operating cash).
func New(limits Limits, src trust.CashSource) *Fake {
	return &Fake{limits: limits, cash: src, accounts: map[trust.AccountID]trust.Account{}, ledgers: map[trust.LedgerID]trust.Ledger{}}
}

// Exceptions returns how many trust exceptions were opened.
func (f *Fake) Exceptions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exceptions
}

func (f *Fake) next(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%d", prefix, f.seq)
}

// OpenAccount opens a trust account.
func (f *Fake) OpenAccount(_ context.Context, _ pgx.Tx, a trust.Account) (trust.AccountID, error) {
	if a.Type != trust.AccountMixed && a.Type != trust.AccountSeparateInterestBearing || a.Title == "" ||
		a.Type == trust.AccountSeparateInterestBearing && a.ForMatterID == "" {
		return "", fmt.Errorf("open account: %w", trust.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	a.ID = trust.AccountID(f.next("acct"))
	a.SignatoryIDs = slices.Clone(a.SignatoryIDs)
	f.accounts[a.ID] = a
	return a.ID, nil
}

// AddSignatory makes userID an active signatory.
func (f *Fake) AddSignatory(_ context.Context, _ pgx.Tx, id trust.AccountID, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[id]
	if !ok {
		return fmt.Errorf("add signatory: %w", trust.ErrNotFound)
	}
	if !slices.Contains(a.SignatoryIDs, userID) {
		a.SignatoryIDs = append(slices.Clone(a.SignatoryIDs), userID)
	}
	f.accounts[id] = a
	return nil
}

// RemoveSignatory deactivates userID as a signatory.
func (f *Fake) RemoveSignatory(_ context.Context, _ pgx.Tx, id trust.AccountID, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[id]
	if !ok {
		return fmt.Errorf("remove signatory: %w", trust.ErrNotFound)
	}
	a.SignatoryIDs = slices.DeleteFunc(slices.Clone(a.SignatoryIDs), func(s string) bool { return s == userID })
	f.accounts[id] = a
	return nil
}

// Account returns one account.
func (f *Fake) Account(_ context.Context, id trust.AccountID) (trust.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[id]
	if !ok {
		return trust.Account{}, fmt.Errorf("account %s: %w", id, trust.ErrNotFound)
	}
	a.SignatoryIDs = slices.Clone(a.SignatoryIDs)
	return a, nil
}

// OpenLedger opens a client ledger.
func (f *Fake) OpenLedger(_ context.Context, _ pgx.Tx, l trust.Ledger) (trust.LedgerID, error) {
	if l.MatterID == "" || l.ClientID == "" || l.ProtectedCents < 0 || l.MinimumBalanceCents < 0 ||
		l.ProtectedCents > 0 && l.ProtectedPurpose == "" {
		return "", fmt.Errorf("open ledger: %w", trust.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.accounts[l.AccountID]; !ok {
		return "", fmt.Errorf("open ledger: account: %w", trust.ErrNotFound)
	}
	for _, o := range f.ledgers {
		if o.AccountID == l.AccountID && o.MatterID == l.MatterID {
			return "", fmt.Errorf("open ledger: %w", trust.ErrDuplicateLedger)
		}
	}
	l.ID = trust.LedgerID(f.next("ledger"))
	f.ledgers[l.ID] = l
	return l.ID, nil
}

// cashCheck counts cash received in trust on matterID plus operating cash plus amount.
// The caller holds f.mu.
func (f *Fake) cashCheck(ctx context.Context, matterID string, amount int64) (trust.CashCheck, error) {
	total := amount
	for _, t := range f.txns {
		if t.Method == trust.MethodCash && t.Kind == trust.KindReceipt && f.ledgers[t.LedgerID].MatterID == matterID {
			total += t.AmountCents
		}
	}
	if f.cash != nil {
		op, err := f.cash.OperatingCashCents(ctx, matterID)
		if err != nil {
			return trust.CashCheck{}, fmt.Errorf("operating cash: %w", err)
		}
		total += op
	}
	c := trust.CashCheck{TotalCents: total}
	if f.limits.CashWarnCents > 0 && total >= f.limits.CashWarnCents {
		c.Warn = true
	}
	if f.limits.CashLimitCents > 0 && total >= f.limits.CashLimitCents {
		c.Blocked = true
	}
	return c, nil
}

// CheckCash reports how cash of amountCents stands against the limit.
func (f *Fake) CheckCash(ctx context.Context, matterID string, amountCents int64) (trust.CashCheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cashCheck(ctx, matterID, amountCents)
}

// Receive posts a receipt.
func (f *Fake) Receive(ctx context.Context, _ pgx.Tx, r trust.Receipt) (trust.TxnID, error) {
	mayBeUncleared := r.Method == trust.MethodCheque || r.Method == trust.MethodBankDraft
	if r.AmountCents <= 0 || r.Counterparty == "" || r.Purpose == "" || r.PostedBy == "" ||
		r.ClearedOn.IsZero() && !mayBeUncleared {
		return "", fmt.Errorf("receive: %w", trust.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.ledgers[r.LedgerID]
	if !ok {
		return "", fmt.Errorf("receive: ledger: %w", trust.ErrNotFound)
	}
	if len(f.accounts[l.AccountID].SignatoryIDs) == 0 {
		return "", fmt.Errorf("receive: %w", trust.ErrNoSignatory)
	}
	if r.Method == trust.MethodCash {
		c, err := f.cashCheck(ctx, l.MatterID, r.AmountCents)
		if err != nil {
			return "", fmt.Errorf("receive: %w", err)
		}
		if c.Blocked && r.CashExceptionCode == "" {
			return "", fmt.Errorf("receive: %w", trust.ErrCashLimit)
		}
		// The fake knows no gates: an exception code without a G6c gate id fails closed.
		if c.Blocked && r.CashGateID == "" {
			return "", fmt.Errorf("receive: %w: %w", trust.ErrCashLimit, trust.ErrCashGateRequired)
		}
	}
	t := trust.Txn{
		ID: trust.TxnID(f.next("txn")), AccountID: l.AccountID, LedgerID: l.ID, Kind: trust.KindReceipt,
		TxnDate: r.TxnDate, Method: r.Method, InstrumentNo: r.InstrumentNo, Counterparty: r.Counterparty,
		AmountCents: r.AmountCents, Purpose: r.Purpose, CashExceptionCode: r.CashExceptionCode,
		ClearedOn: r.ClearedOn, PostedBy: r.PostedBy,
	}
	f.txns = append(f.txns, t)
	return t.ID, nil
}

// balance computes a ledger's balance. The caller holds f.mu.
func (f *Fake) balance(l trust.Ledger) trust.Balance {
	b := trust.Balance{LedgerID: l.ID, ProtectedCents: l.ProtectedCents, MinimumCents: l.MinimumBalanceCents}
	for _, t := range f.txns {
		if t.LedgerID != l.ID {
			continue
		}
		b.PostedCents += t.AmountCents
		if t.AmountCents < 0 || !t.ClearedOn.IsZero() {
			b.ClearedCents += t.AmountCents
		}
	}
	b.AvailableCents = trust.AvailableCents(b.ClearedCents, b.ProtectedCents, b.MinimumCents, 0)
	return b
}

// Balance returns a ledger's balance.
func (f *Fake) Balance(_ context.Context, id trust.LedgerID) (trust.Balance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.ledgers[id]
	if !ok {
		return trust.Balance{}, fmt.Errorf("balance %s: %w", id, trust.ErrNotFound)
	}
	return f.balance(l), nil
}

// LockLedger returns a ledger's balance under the fake's mutex. The fake keeps no withdrawal
// holds, so the hold total is always 0.
func (f *Fake) LockLedger(ctx context.Context, _ pgx.Tx, id trust.LedgerID) (trust.Balance, int64, error) {
	b, err := f.Balance(ctx, id)
	return b, 0, err
}

// Txns lists a ledger's transactions in posting order.
func (f *Fake) Txns(_ context.Context, id trust.LedgerID) ([]trust.Txn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.ledgers[id]; !ok {
		return nil, fmt.Errorf("txns %s: %w", id, trust.ErrNotFound)
	}
	var out []trust.Txn
	for _, t := range f.txns {
		if t.LedgerID == id {
			out = append(out, t)
		}
	}
	return out, nil
}

// Disburse re-checks the available balance under the lock and posts the disbursement.
func (f *Fake) Disburse(_ context.Context, _ pgx.Tx, d trust.Disbursement) (trust.TxnID, error) {
	if d.AmountCents <= 0 || d.WithdrawalID == "" || d.Counterparty == "" || d.PostedBy == "" {
		return "", fmt.Errorf("disburse: %w", trust.ErrInvalid)
	}
	if d.Method == trust.MethodCash {
		return "", fmt.Errorf("disburse: %w", trust.ErrCashPayout)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.ledgers[d.LedgerID]
	if !ok {
		return "", fmt.Errorf("disburse: ledger: %w", trust.ErrNotFound)
	}
	b := f.balance(l)
	if d.AmountCents > b.AvailableCents {
		if d.AmountCents <= trust.AvailableCents(b.PostedCents, b.ProtectedCents, b.MinimumCents, 0) {
			return "", fmt.Errorf("disburse: %w", trust.ErrUnclearedFunds)
		}
		return "", fmt.Errorf("disburse: %w", trust.ErrInsufficientFunds)
	}
	t := trust.Txn{
		ID: trust.TxnID(f.next("txn")), AccountID: l.AccountID, LedgerID: l.ID, Kind: trust.KindDisbursement,
		TxnDate: d.TxnDate, Method: d.Method, Counterparty: d.Counterparty, AmountCents: -d.AmountCents,
		Purpose: d.Purpose, WithdrawalID: d.WithdrawalID, ApprovalID: d.ApprovalID, ClearedOn: d.TxnDate, PostedBy: d.PostedBy,
	}
	f.txns = append(f.txns, t)
	return t.ID, nil
}

// ReverseNSF reverses a returned receipt and replenishes any shortfall from firm funds.
func (f *Fake) ReverseNSF(_ context.Context, _ pgx.Tx, receipt trust.TxnID, approvalID, by string) (trust.NSFResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.txns, func(t trust.Txn) bool { return t.ID == receipt })
	if i < 0 {
		return trust.NSFResult{}, fmt.Errorf("reverse nsf: %w", trust.ErrNotFound)
	}
	orig := f.txns[i]
	if orig.Kind != trust.KindReceipt {
		return trust.NSFResult{}, fmt.Errorf("reverse nsf: %w", trust.ErrNotReceipt)
	}
	if slices.ContainsFunc(f.txns, func(t trust.Txn) bool { return t.ReversesID == receipt }) {
		return trust.NSFResult{}, fmt.Errorf("reverse nsf: %w", trust.ErrAlreadyReversed)
	}
	l := f.ledgers[orig.LedgerID]
	posted := f.balance(l).PostedCents
	shortfall := orig.AmountCents - posted
	if shortfall > 0 && approvalID == "" {
		return trust.NSFResult{}, fmt.Errorf("reverse nsf: replenishment needs approval: %w", trust.ErrInvalid)
	}
	rev := trust.Txn{
		ID: trust.TxnID(f.next("txn")), AccountID: l.AccountID, LedgerID: l.ID, Kind: trust.KindReversal,
		TxnDate: orig.TxnDate, Method: orig.Method, Counterparty: orig.Counterparty, AmountCents: -orig.AmountCents,
		Purpose: "NSF reversal", ReversesID: orig.ID, ClearedOn: orig.TxnDate, PostedBy: by,
	}
	f.txns = append(f.txns, rev)
	res := trust.NSFResult{ReversalID: rev.ID}
	if shortfall > 0 {
		rep := trust.Txn{
			ID: trust.TxnID(f.next("txn")), AccountID: l.AccountID, LedgerID: l.ID, Kind: trust.KindReceipt,
			TxnDate: orig.TxnDate, Method: trust.MethodInternal, Counterparty: "general account", AmountCents: shortfall,
			Purpose: "NSF replenishment", ReplenishesID: rev.ID, ApprovalID: approvalID, ClearedOn: orig.TxnDate, PostedBy: by,
		}
		f.txns = append(f.txns, rep)
		f.exceptions++
		res.ReplenishmentID, res.ShortfallCents, res.ExceptionOpened = rep.ID, shortfall, true
	}
	return res, nil
}
