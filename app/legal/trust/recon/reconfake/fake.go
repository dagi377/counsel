// Package reconfake is an in-memory recon.Service for tests. No network, Postgres or Docker.
package reconfake

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/trust/recon"
)

var _ recon.Service = (*Fake)(nil)

type acctPeriod struct {
	account string
	period  recon.Period
}

type book struct{ book, clients int64 }

// Fake is a concurrency-safe in-memory recon.Service.
type Fake struct {
	// Now returns the current time; nil means time.Now.
	Now func() time.Time

	mu         sync.Mutex
	seq        int
	statements map[acctPeriod]recon.Statement
	books      map[acctPeriod]book
	recs       map[string]*recon.Reconciliation
	locked     map[acctPeriod]bool
}

// New returns an empty Fake.
func New() *Fake {
	return &Fake{
		statements: map[acctPeriod]recon.Statement{},
		books:      map[acctPeriod]book{},
		recs:       map[string]*recon.Reconciliation{},
		locked:     map[acctPeriod]bool{},
	}
}

// SetBook sets the month-end trust book balance and the sum of client ledgers, which the
// real service reads from the trust journal.
func (f *Fake) SetBook(accountID string, p recon.Period, bookCents, clientLedgerCents int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.books[acctPeriod{accountID, p}] = book{bookCents, clientLedgerCents}
}

func (f *Fake) nextID(prefix string) string {
	f.seq++
	return prefix + strconv.Itoa(f.seq)
}

// ImportStatement implements recon.Service.
func (f *Fake) ImportStatement(ctx context.Context, s recon.Statement) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sum := s.OpeningCents
	for _, l := range s.Lines {
		sum += l.AmountCents
	}
	if sum != s.ClosingCents {
		return "", fmt.Errorf("import statement: %w", recon.ErrStatementMismatch)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	k := acctPeriod{s.AccountID, s.Period}
	if f.locked[k] {
		return "", fmt.Errorf("import statement: %w", recon.ErrPeriodLocked)
	}
	if _, ok := f.statements[k]; ok {
		return "", fmt.Errorf("import statement: %w", recon.ErrDuplicateStatement)
	}
	s.ID = f.nextID("stmt-")
	s.Lines = append([]recon.StatementLine(nil), s.Lines...)
	f.statements[k] = s
	return s.ID, nil
}

// Closing returns the closing balance of the imported statement for the account's month, or
// recon.ErrNotFound (the recon.Statements port).
func (f *Fake) Closing(ctx context.Context, accountID string, p recon.Period) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.statements[acctPeriod{accountID, p}]
	if !ok {
		return 0, fmt.Errorf("closing: statement: %w", recon.ErrNotFound)
	}
	return st.ClosingCents, nil
}

// Prepare implements recon.Service.
func (f *Fake) Prepare(ctx context.Context, accountID string, p recon.Period, items []recon.ReconcilingItem, preparedBy string) (recon.Reconciliation, error) {
	if err := ctx.Err(); err != nil {
		return recon.Reconciliation{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	k := acctPeriod{accountID, p}
	if f.locked[k] {
		return recon.Reconciliation{}, fmt.Errorf("prepare: %w", recon.ErrPeriodLocked)
	}
	st, ok := f.statements[k]
	if !ok {
		return recon.Reconciliation{}, fmt.Errorf("prepare: statement: %w", recon.ErrNotFound)
	}
	b := f.books[k]
	tw := recon.ThreeWay{
		BankCents:         st.ClosingCents,
		AdjustedBankCents: st.ClosingCents,
		BookCents:         b.book,
		ClientLedgerCents: b.clients,
		Items:             append([]recon.ReconcilingItem(nil), items...),
	}
	for _, it := range items {
		tw.AdjustedBankCents += it.AmountCents
	}
	r := &recon.Reconciliation{
		ID: f.nextID("rec-"), AccountID: accountID, Period: p, Result: tw,
		Status: recon.StatusPrepared, PreparedBy: preparedBy,
	}
	f.recs[r.ID] = r
	return *r, nil
}

// Approve implements recon.Service.
func (f *Fake) Approve(ctx context.Context, id string, by recon.Approver) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recs[id]
	if !ok {
		return fmt.Errorf("approve %s: %w", id, recon.ErrNotFound)
	}
	switch {
	case by.IsAgent:
		return fmt.Errorf("approve %s: %w", id, recon.ErrAgentApproval)
	case by.UserID == "" || by.UserID == r.PreparedBy:
		return fmt.Errorf("approve %s: %w", id, recon.ErrSelfApproval)
	case r.Status != recon.StatusPrepared:
		return fmt.Errorf("approve %s: %w", id, recon.ErrInvalidState)
	case !r.Result.Balanced():
		return fmt.Errorf("approve %s: %w", id, recon.ErrUnbalanced)
	}
	now := time.Now()
	if f.Now != nil {
		now = f.Now()
	}
	r.Status, r.ApprovedBy, r.ApprovedAt = recon.StatusApproved, by.UserID, &now
	f.locked[acctPeriod{r.AccountID, r.Period}] = true
	return nil
}

// Get implements recon.Service.
func (f *Fake) Get(ctx context.Context, id string) (recon.Reconciliation, error) {
	if err := ctx.Err(); err != nil {
		return recon.Reconciliation{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recs[id]
	if !ok {
		return recon.Reconciliation{}, fmt.Errorf("get %s: %w", id, recon.ErrNotFound)
	}
	return *r, nil
}

// Locked implements recon.Service.
func (f *Fake) Locked(ctx context.Context, accountID string, p recon.Period) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.locked[acctPeriod{accountID, p}], nil
}
