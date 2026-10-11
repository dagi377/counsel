package ledger

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/registry"
)

// ErrInvalidChart means a registered chart has no book, a duplicate book, an account with
// no code or name, an unknown account type, or an account code used twice.
var ErrInvalidChart = errors.New("ledger: invalid chart of accounts")

// ErrChartConflict means a registered account code already exists in gl.account with a
// different type.
var ErrChartConflict = errors.New("ledger: account type differs from stored account")

var accountTypes = map[registry.AccountType]bool{
	registry.AccountAsset: true, registry.AccountLiability: true, registry.AccountEquity: true,
	registry.AccountRevenue: true, registry.AccountExpense: true,
}

type chartAccount struct {
	book string
	acct registry.Account
}

// Charts is the set of charts of accounts the app registered at boot
// (Registry.ChartOfAccounts). The engine knows no account code of its own: every code it
// accepts comes from here.
type Charts struct {
	byCode map[string]chartAccount
	books  map[string]bool
}

// NewCharts validates cs and indexes their accounts by code. Codes are unique across books
// because gl.account.code is unique.
func NewCharts(cs ...registry.Chart) (*Charts, error) {
	c := &Charts{byCode: map[string]chartAccount{}, books: map[string]bool{}}
	for _, ch := range cs {
		if ch.Book == "" || c.books[ch.Book] {
			return nil, fmt.Errorf("chart %q: book: %w", ch.Book, ErrInvalidChart)
		}
		c.books[ch.Book] = true
		for _, a := range ch.Accounts {
			if a.Code == "" || a.Name == "" || !accountTypes[a.Type] {
				return nil, fmt.Errorf("chart %q account %q: %w", ch.Book, a.Code, ErrInvalidChart)
			}
			if prev, ok := c.byCode[a.Code]; ok {
				return nil, fmt.Errorf("chart %q account %q already in chart %q: %w", ch.Book, a.Code, prev.book, ErrInvalidChart)
			}
			c.byCode[a.Code] = chartAccount{book: ch.Book, acct: a}
		}
	}
	return c, nil
}

// Account returns the registered account for code and the book it belongs to.
func (c *Charts) Account(code string) (registry.Account, string, bool) {
	a, ok := c.byCode[code]
	return a.acct, a.book, ok
}

// Codes returns every registered account code in sorted order.
func (c *Charts) Codes() []string {
	out := make([]string, 0, len(c.byCode))
	for code := range c.byCode {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

// Check reports ErrUnknownAccount when a line of e names an account no chart registers, or
// an account registered under a different book than e.Book.
func (c *Charts) Check(e Entry) error {
	if !c.books[e.Book] {
		return fmt.Errorf("entry %s/%s book %q: %w", e.SourceKind, e.SourceID, e.Book, ErrUnknownAccount)
	}
	for i, l := range e.Lines {
		a, ok := c.byCode[l.Account]
		if !ok || a.book != e.Book {
			return fmt.Errorf("entry %s/%s line %d account %q: %w", e.SourceKind, e.SourceID, i, l.Account, ErrUnknownAccount)
		}
	}
	return nil
}

// Statements Sync issues. Sync inserts missing accounts and never rewrites a stored type.
const (
	sqlInsertAccount = `INSERT INTO gl.account (code, name, type, book) VALUES ($1, $2, $3, $4)
ON CONFLICT (code) DO NOTHING`
	sqlAccountType = `SELECT type FROM gl.account WHERE code = $1`
)

// syncStatements lists every statement Sync may run, for the no-mutation check.
var syncStatements = []string{sqlInsertAccount, sqlAccountType}

// Sync writes every registered account into gl.account inside tx. An account that already
// exists keeps its row; a stored type that differs from the registered one is
// ErrChartConflict.
func (c *Charts) Sync(ctx context.Context, tx pgx.Tx) error {
	for _, code := range c.Codes() {
		ca := c.byCode[code]
		a := ca.acct
		if _, err := tx.Exec(ctx, sqlInsertAccount, a.Code, a.Name, string(a.Type), ca.book); err != nil {
			return fmt.Errorf("sync account %q: insert: %w", a.Code, err)
		}
		var typ string
		if err := tx.QueryRow(ctx, sqlAccountType, a.Code).Scan(&typ); err != nil {
			return fmt.Errorf("sync account %q: read type: %w", a.Code, err)
		}
		if typ != string(a.Type) {
			return fmt.Errorf("sync account %q: stored %q, registered %q: %w", a.Code, typ, a.Type, ErrChartConflict)
		}
	}
	return nil
}
