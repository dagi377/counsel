package books

import (
	"fmt"
	"slices"

	"github.com/dagi377/counsel/kernel/registry"
)

// chart is the b3 chart_of_accounts, ordered by book then code. Type keeps the contra
// classes; ledgerType maps them to the ledger class the kernel registers.
var chart = []Account{
	{Book: BookOperating, Code: AcctOperatingBank, Name: "Operating bank", Type: "asset"},
	{Book: BookOperating, Code: AcctProcessorClearing, Name: "Processor clearing", Type: "asset"},
	{Book: BookOperating, Code: AcctARFees, Name: "AR fees and disbursements", Type: "asset"},
	{Book: BookOperating, Code: AcctARInterest, Name: "AR interest", Type: "asset"},
	{Book: BookOperating, Code: AcctWIP, Name: "WIP unbilled time, at recorded value", Type: "asset"},
	{Book: BookOperating, Code: AcctDisbRecoverable, Name: "Disbursements recoverable", Type: "asset"},
	{Book: BookOperating, Code: AcctITCRecoverable, Name: "HST ITC recoverable", Type: "asset"},
	{Book: BookOperating, Code: AcctWIPOffset, Name: "WIP offset", Type: "contra-asset"},
	{Book: BookOperating, Code: AcctSalesTaxPayable, Name: "GST/HST payable", Type: "liability"},
	{Book: BookOperating, Code: AcctProvincialTax, Name: "QST / PST payable", Type: "liability"},
	{Book: BookOperating, Code: AcctClientCredit, Name: "Client credit (unapplied)", Type: "liability"},
	{Book: BookOperating, Code: AcctDueToTrust, Name: "Due to trust", Type: "liability"},
	{Book: BookOperating, Code: AcctTrustInOperating, Name: "Trust money held in operating", Type: "liability"},
	{Book: BookOperating, Code: AcctFeeRevenue, Name: "Fee revenue", Type: "revenue"},
	{Book: BookOperating, Code: AcctCostRecoveryRevenue, Name: "Cost recovery revenue", Type: "revenue"},
	{Book: BookOperating, Code: AcctInterestIncome, Name: "Interest income", Type: "revenue"},
	{Book: BookOperating, Code: AcctSurchargeRevenue, Name: "Card surcharge revenue", Type: "revenue"},
	{Book: BookOperating, Code: AcctFeeAdjustments, Name: "Fee adjustments", Type: "contra-revenue"},
	{Book: BookOperating, Code: AcctBadDebt, Name: "Bad debt expense", Type: "expense"},
	{Book: BookOperating, Code: AcctProcessingFees, Name: "Payment processing fees", Type: "expense"},
	{Book: BookTrust, Code: AcctTrustBank, Name: "Trust bank", Type: "asset"},
	{Book: BookTrust, Code: AcctClientTrustLedger, Name: "Client trust ledger", Type: "liability"},
}

// ledgerType maps a chart type to the kernel ledger class; a contra account keeps the
// class of the account it offsets.
var ledgerType = map[string]registry.AccountType{
	"asset":          registry.AccountAsset,
	"contra-asset":   registry.AccountAsset,
	"liability":      registry.AccountLiability,
	"equity":         registry.AccountEquity,
	"revenue":        registry.AccountRevenue,
	"contra-revenue": registry.AccountRevenue,
	"expense":        registry.AccountExpense,
}

// RegisterCharts registers the operating and trust charts as two separate ledger books.
// It is not yet added to Register: app/legal's TestRegisterWithNoFeaturesRecordsNothing
// expects no registry calls (Q-005).
// rule: BIL-I-002
func RegisterCharts(r registry.Registry) error {
	for _, book := range []string{BookOperating, BookTrust} {
		c := registry.Chart{Book: book}
		for _, a := range chart {
			if a.Book != book {
				continue
			}
			t, ok := ledgerType[a.Type]
			if !ok {
				return fmt.Errorf("chart %s: account %s type %q: %w", book, a.Code, a.Type, ErrUnknownAccount)
			}
			c.Accounts = append(c.Accounts, registry.Account{Code: a.Code, Name: a.Name, Type: t})
		}
		r.ChartOfAccounts(c)
	}
	return nil
}

// Charts is the firm's operating and trust charts of accounts.
type Charts struct{}

var _ Chart = Charts{}

// Accounts returns every account of both books, ordered by book then code.
func (Charts) Accounts() []Account {
	out := slices.Clone(chart)
	slices.SortFunc(out, func(a, b Account) int {
		if a.Book != b.Book {
			if a.Book < b.Book {
				return -1
			}
			return 1
		}
		if a.Code < b.Code {
			return -1
		}
		if a.Code > b.Code {
			return 1
		}
		return 0
	})
	return out
}

func bookOf(code string) (string, bool) {
	for _, a := range chart {
		if a.Code == code {
			return a.Book, true
		}
	}
	return "", false
}

// CheckEntry checks e against the charts: every account exists and belongs to e.Book, each
// line has exactly one positive side, there are at least two lines and debits equal credits.
func (Charts) CheckEntry(e Entry) error {
	if e.Book != BookOperating && e.Book != BookTrust {
		return fmt.Errorf("entry book %q: %w", e.Book, ErrUnknownAccount)
	}
	var dr, cr int64
	for i, l := range e.Lines {
		book, ok := bookOf(l.Account)
		if !ok {
			return fmt.Errorf("line %d account %q: %w", i, l.Account, ErrUnknownAccount)
		}
		// rule: BIL-I-002
		if book != e.Book {
			return fmt.Errorf("line %d account %s is in the %s book, entry is %s: %w", i, l.Account, book, e.Book, ErrCrossBook)
		}
		if l.DebitCents < 0 || l.CreditCents < 0 || (l.DebitCents == 0) == (l.CreditCents == 0) {
			return fmt.Errorf("line %d needs exactly one positive side: %w", i, ErrUnbalanced)
		}
		dr += l.DebitCents
		cr += l.CreditCents
	}
	if len(e.Lines) < 2 || dr != cr {
		return fmt.Errorf("entry %s: %d lines, debits %d, credits %d: %w", e.Book, len(e.Lines), dr, cr, ErrUnbalanced)
	}
	return nil
}
