package timekeeping

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
)

// ExpenseID identifies an expense (timekeeping.expense.id).
type ExpenseID string

// ExpenseStatus is timekeeping.expense.status.
type ExpenseStatus string

// Expense statuses this service writes. The rest of the DDL set (approved, on_draft_bill,
// billed, ...) is set by approval and billing, never by recording.
const (
	ExpenseDraft ExpenseStatus = "draft"
)

// PaidFrom values, from the timekeeping.expense.paid_from CHECK.
var paidFromValues = map[string]bool{"operating": true, "trust": true, "personal": true, "card": true, "unpaid": true}

// Expense is one recorded disbursement (timekeeping.expense). Money is integer cents.
type Expense struct {
	ID                ExpenseID
	MatterID          string
	UserID            string // who incurred the expense
	ExpenseDate       time.Time
	Vendor            string
	AmountPreTaxCents int64
	VendorTaxCents    int64
	IsAgency          bool
	PaidFrom          string
	Billable          bool
	Status            ExpenseStatus
	BillID            string // set while the expense is on a non-void bill
	RowVersion        int
}

// ExpenseFilter selects one page of Expenses.List.
type ExpenseFilter struct {
	UserID string // empty: every user's expenses
	After  ExpenseID
	Limit  int
}

// Expenses records expenses and reads them back.
type Expenses interface {
	// Record saves e as a draft expense incurred by by.UserID. An agent never records.
	Record(ctx context.Context, tx pgx.Tx, e Expense, by Actor) (ExpenseID, error)
	// Get returns one expense, or ErrNotFound.
	Get(ctx context.Context, id ExpenseID) (Expense, error)
	// List returns expenses by id after f.After, at most f.Limit, only f.UserID's when set.
	List(ctx context.Context, f ExpenseFilter) ([]Expense, error)
}

// ValidateExpense checks the fields timekeeping.expense constrains. Errors name the field,
// never its value.
func ValidateExpense(e Expense) error {
	switch {
	case e.MatterID == "" || e.UserID == "":
		return fmt.Errorf("matter and user required: %w", ErrInvalid)
	case e.ExpenseDate.IsZero():
		return fmt.Errorf("expense date required: %w", ErrInvalid)
	case e.AmountPreTaxCents <= 0:
		return fmt.Errorf("amount must be > 0: %w", ErrInvalid)
	case e.VendorTaxCents < 0:
		return fmt.Errorf("vendor tax must be >= 0: %w", ErrInvalid)
	case !paidFromValues[e.PaidFrom]:
		return fmt.Errorf("paid_from not allowed: %w", ErrInvalid)
	}
	return nil
}

// timekeeping.expense is owned by this module; the store touches no other table.
const (
	expenseCols = `id::text, matter_id::text, user_id::text, expense_date, coalesce(vendor, ''),
  amount_pre_tax_cents, vendor_tax_cents, is_agency, paid_from, billable, status,
  coalesce(bill_id::text, ''), row_version`
	sqlInsertExpense = `INSERT INTO timekeeping.expense
  (matter_id, user_id, expense_date, vendor, amount_pre_tax_cents, vendor_tax_cents, is_agency,
   paid_from, billable, created_by, updated_by)
VALUES ($1::uuid, $2::uuid, $3::date, NULLIF($4, ''), $5, $6, $7, $8, $9, $2::uuid, $2::uuid)
RETURNING id::text`
	sqlGetExpense   = `SELECT ` + expenseCols + ` FROM timekeeping.expense WHERE id = $1::uuid`
	sqlListExpenses = `SELECT ` + expenseCols + ` FROM timekeeping.expense
WHERE ($1 = '' OR user_id = NULLIF($1, '')::uuid) AND ($2 = '' OR id > NULLIF($2, '')::uuid)
ORDER BY id LIMIT $3`
)

// pgInvalidText is the Postgres SQLSTATE for a malformed value, such as an id that is not a uuid.
const pgInvalidText = "22P02"

// ExpenseStore is the Postgres Expenses over timekeeping.expense. Rows are never deleted.
type ExpenseStore struct {
	db  db.DB
	loc *time.Location
}

var _ Expenses = (*ExpenseStore)(nil)

// NewExpenseStore returns a store on pool. loc is the business time zone expense dates are
// read in (America/Toronto).
func NewExpenseStore(pool db.DB, loc *time.Location) *ExpenseStore {
	return &ExpenseStore{db: pool, loc: loc}
}

func (s *ExpenseStore) scan(row pgx.Row) (Expense, error) {
	var e Expense
	var day time.Time
	var status string
	if err := row.Scan(&e.ID, &e.MatterID, &e.UserID, &day, &e.Vendor, &e.AmountPreTaxCents, &e.VendorTaxCents,
		&e.IsAgency, &e.PaidFrom, &e.Billable, &status, &e.BillID, &e.RowVersion); err != nil {
		return Expense{}, err
	}
	e.ExpenseDate = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, s.loc)
	e.Status = ExpenseStatus(status)
	return e, nil
}

// expenseNotFound maps a missing row or a malformed id to ErrNotFound.
func expenseNotFound(err error) error {
	var pg *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == pgInvalidText) {
		return ErrNotFound
	}
	return err
}

// Record implements Expenses inside the caller's tx.
func (s *ExpenseStore) Record(ctx context.Context, tx pgx.Tx, e Expense, by Actor) (ExpenseID, error) {
	// rule: ARC-I-051
	// An agent only drafts through suggestions; it never records an expense itself.
	if by.Agent {
		return "", fmt.Errorf("record expense: %w", ErrAgentForbidden)
	}
	e.UserID = by.UserID
	if err := ValidateExpense(e); err != nil {
		return "", fmt.Errorf("record expense: %w", err)
	}
	var id string
	err := tx.QueryRow(ctx, sqlInsertExpense, e.MatterID, e.UserID, e.ExpenseDate.In(s.loc).Format(time.DateOnly),
		strings.TrimSpace(e.Vendor), e.AmountPreTaxCents, e.VendorTaxCents, e.IsAgency, e.PaidFrom, e.Billable).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("record expense: %w", err)
	}
	return ExpenseID(id), nil
}

// Get implements Expenses.
func (s *ExpenseStore) Get(ctx context.Context, id ExpenseID) (Expense, error) {
	var out Expense
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.scan(tx.QueryRow(ctx, sqlGetExpense, string(id)))
		return expenseNotFound(err)
	})
	if err != nil {
		return Expense{}, fmt.Errorf("get expense: %w", err)
	}
	return out, nil
}

// List implements Expenses.
func (s *ExpenseStore) List(ctx context.Context, f ExpenseFilter) ([]Expense, error) {
	out := []Expense{}
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlListExpenses, f.UserID, string(f.After), f.Limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e, err := s.scan(rows)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list expenses: %w", expenseNotFound(err))
	}
	return out, nil
}
