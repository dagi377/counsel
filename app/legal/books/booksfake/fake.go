// Package booksfake is an in-memory books.Chart, books.Reconciler and books.VendorBills.
package booksfake

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/dagi377/counsel/app/legal/books"
)

var (
	_ books.Chart       = (*Fake)(nil)
	_ books.Reconciler  = (*Fake)(nil)
	_ books.VendorBills = (*Fake)(nil)
)

var accounts = []books.Account{
	{Book: books.BookOperating, Code: books.AcctOperatingBank, Name: "Operating bank", Type: "asset"},
	{Book: books.BookOperating, Code: books.AcctProcessorClearing, Name: "Processor clearing", Type: "asset"},
	{Book: books.BookOperating, Code: books.AcctARFees, Name: "AR fees and disbursements", Type: "asset"},
	{Book: books.BookOperating, Code: books.AcctARInterest, Name: "AR interest", Type: "asset"},
	{Book: books.BookOperating, Code: books.AcctWIP, Name: "WIP unbilled time, at recorded value", Type: "asset"},
	{Book: books.BookOperating, Code: books.AcctDisbRecoverable, Name: "Disbursements recoverable", Type: "asset"},
	{Book: books.BookOperating, Code: books.AcctITCRecoverable, Name: "HST ITC recoverable", Type: "asset"},
	{Book: books.BookOperating, Code: books.AcctWIPOffset, Name: "WIP offset", Type: "contra-asset"},
	{Book: books.BookOperating, Code: books.AcctSalesTaxPayable, Name: "GST/HST payable", Type: "liability"},
	{Book: books.BookOperating, Code: books.AcctProvincialTax, Name: "QST / PST payable", Type: "liability"},
	{Book: books.BookOperating, Code: books.AcctClientCredit, Name: "Client credit (unapplied)", Type: "liability"},
	{Book: books.BookOperating, Code: books.AcctDueToTrust, Name: "Due to trust", Type: "liability"},
	{Book: books.BookOperating, Code: books.AcctTrustInOperating, Name: "Trust money held in operating", Type: "liability"},
	{Book: books.BookOperating, Code: books.AcctFeeRevenue, Name: "Fee revenue", Type: "revenue"},
	{Book: books.BookOperating, Code: books.AcctCostRecoveryRevenue, Name: "Cost recovery revenue", Type: "revenue"},
	{Book: books.BookOperating, Code: books.AcctInterestIncome, Name: "Interest income", Type: "revenue"},
	{Book: books.BookOperating, Code: books.AcctSurchargeRevenue, Name: "Card surcharge revenue", Type: "revenue"},
	{Book: books.BookOperating, Code: books.AcctFeeAdjustments, Name: "Fee adjustments", Type: "contra-revenue"},
	{Book: books.BookOperating, Code: books.AcctBadDebt, Name: "Bad debt expense", Type: "expense"},
	{Book: books.BookOperating, Code: books.AcctProcessingFees, Name: "Payment processing fees", Type: "expense"},
	{Book: books.BookTrust, Code: books.AcctTrustBank, Name: "Trust bank", Type: "asset"},
	{Book: books.BookTrust, Code: books.AcctClientTrustLedger, Name: "Client trust ledger", Type: "liability"},
}

// Fake keeps vendor bills in memory and uses the b3 chart of accounts.
type Fake struct {
	mu    sync.Mutex
	bills map[string]books.VendorBill
	next  int
}

// New returns an empty Fake.
func New() *Fake { return &Fake{bills: map[string]books.VendorBill{}} }

// Accounts implements books.Chart.
func (f *Fake) Accounts() []books.Account {
	out := make([]books.Account, len(accounts))
	copy(out, accounts)
	return out
}

func bookOf(code string) (string, bool) {
	for _, a := range accounts {
		if a.Code == code {
			return a.Book, true
		}
	}
	return "", false
}

// CheckEntry implements books.Chart.
func (f *Fake) CheckEntry(e books.Entry) error {
	var dr, cr int64
	for _, l := range e.Lines {
		book, ok := bookOf(l.Account)
		if !ok {
			return fmt.Errorf("check entry: %s: %w", l.Account, books.ErrUnknownAccount)
		}
		// rule: BIL-I-002
		if book != e.Book {
			return fmt.Errorf("check entry: %s in %s book: %w", l.Account, e.Book, books.ErrCrossBook)
		}
		dr += l.DebitCents
		cr += l.CreditCents
	}
	if dr != cr || len(e.Lines) == 0 {
		return fmt.Errorf("check entry: debits %d credits %d: %w", dr, cr, books.ErrUnbalanced)
	}
	return nil
}

// Reconcile implements books.Reconciler.
func (f *Fake) Reconcile(_ context.Context, s books.Snapshot) ([]books.Mismatch, error) {
	// rule: BIL-I-013
	checks := []books.Mismatch{
		{Rule: "BIL-I-013", Account: books.AcctARFees + "+" + books.AcctARInterest, LedgerCents: s.ARBalanceCents, SubCents: s.OpenBillBalancesCents},
		{Rule: "BIL-I-013", Account: books.AcctClientCredit, LedgerCents: s.ClientCreditLedger, SubCents: s.ClientCreditSubledger},
		{Rule: "BIL-I-013", Account: books.AcctWIP, LedgerCents: s.WIPLedgerCents, SubCents: s.UnbilledTimeValueCents},
	}
	var out []books.Mismatch
	for _, c := range checks {
		if c.LedgerCents != c.SubCents {
			out = append(out, c)
		}
	}
	if len(out) > 0 {
		return out, fmt.Errorf("reconcile: %d mismatches: %w", len(out), books.ErrMismatch)
	}
	return nil, nil
}

// Create implements books.VendorBills.
func (f *Fake) Create(_ context.Context, b books.VendorBill) (books.VendorBill, error) {
	if b.AmountCents <= 0 || b.TaxCents < 0 || strings.TrimSpace(b.Vendor) == "" || b.BillDate.IsZero() {
		return books.VendorBill{}, fmt.Errorf("create vendor bill: %w", books.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	b.ID = fmt.Sprintf("vb-%d", f.next)
	b.Status = books.VendorBillOpen
	f.bills[b.ID] = b
	return b, nil
}

// Get implements books.VendorBills.
func (f *Fake) Get(_ context.Context, id string) (books.VendorBill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.bills[id]
	if !ok {
		return books.VendorBill{}, fmt.Errorf("get vendor bill %s: %w", id, books.ErrNotFound)
	}
	return b, nil
}

func (f *Fake) move(id string, to books.VendorBillStatus) (books.VendorBill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.bills[id]
	if !ok {
		return books.VendorBill{}, fmt.Errorf("vendor bill %s: %w", id, books.ErrNotFound)
	}
	if b.Status != books.VendorBillOpen {
		return books.VendorBill{}, fmt.Errorf("vendor bill %s %s -> %s: %w", id, b.Status, to, books.ErrBadStatus)
	}
	b.Status = to
	f.bills[id] = b
	return b, nil
}

// MarkPaid implements books.VendorBills.
func (f *Fake) MarkPaid(_ context.Context, id string) (books.VendorBill, error) {
	return f.move(id, books.VendorBillPaid)
}

// Void implements books.VendorBills.
func (f *Fake) Void(_ context.Context, id string) (books.VendorBill, error) {
	return f.move(id, books.VendorBillVoid)
}
