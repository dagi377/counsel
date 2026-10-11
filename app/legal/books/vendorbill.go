package books

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

// gl.vendor_bill is an app-layer table in the kernel gl schema (spec/eng/a4-data-model.json:
// layer app, module gl). This package owns it and is the only writer. Its statements touch no
// other table.
const (
	vendorBillCols = `id::text, vendor, bill_date, due_date, amount_cents, tax_cents,
  coalesce(matter_id::text, ''), coalesce(document_id::text, ''), status`
	sqlInsertVendorBill = `INSERT INTO gl.vendor_bill
  (vendor, bill_date, due_date, amount_cents, tax_cents, matter_id, document_id)
VALUES ($1, $2::date, $3::date, $4, $5, NULLIF($6, '')::uuid, NULLIF($7, '')::uuid)
RETURNING ` + vendorBillCols
	sqlGetVendorBill = `SELECT ` + vendorBillCols + ` FROM gl.vendor_bill WHERE id = $1::uuid`
	// The status move is guarded in SQL so two racing moves cannot both succeed.
	sqlMoveVendorBill = `UPDATE gl.vendor_bill SET status = $2
WHERE id = $1::uuid AND status = 'open'
RETURNING ` + vendorBillCols
)

// vendorBillStatements lists every statement the store runs, for the ownership check.
var vendorBillStatements = []string{sqlInsertVendorBill, sqlGetVendorBill, sqlMoveVendorBill}

// pgInvalidText is the Postgres SQLSTATE for a malformed value, such as an id that is not a uuid.
const pgInvalidText = "22P02"

// VendorBillStore is the Postgres VendorBills over gl.vendor_bill. Rows are never deleted;
// a wrong bill is voided.
type VendorBillStore struct {
	db  db.DB
	loc *time.Location
}

var _ VendorBills = (*VendorBillStore)(nil)

// NewVendorBillStore returns a store on pool. loc is the business time zone that bill and
// due dates are read in (America/Toronto).
func NewVendorBillStore(pool db.DB, loc *time.Location) *VendorBillStore {
	return &VendorBillStore{db: pool, loc: loc}
}

// validateVendorBill checks the fields gl.vendor_bill constrains. Errors name the field,
// never its value.
func validateVendorBill(b VendorBill) error {
	switch {
	case strings.TrimSpace(b.Vendor) == "":
		return fmt.Errorf("vendor required: %w", ErrInvalid)
	case b.BillDate.IsZero():
		return fmt.Errorf("bill date required: %w", ErrInvalid)
	case b.AmountCents <= 0:
		return fmt.Errorf("amount must be > 0: %w", ErrInvalid)
	case b.TaxCents < 0:
		return fmt.Errorf("tax must be >= 0: %w", ErrInvalid)
	}
	return nil
}

// civil formats t as a calendar date in loc.
func civil(t time.Time, loc *time.Location) string { return t.In(loc).Format(time.DateOnly) }

// nextStatus returns the status a move to want leaves the bill in, or ErrBadStatus.
// Only open bills move, and only to paid or void.
func nextStatus(from, want VendorBillStatus) (VendorBillStatus, error) {
	if from != VendorBillOpen || (want != VendorBillPaid && want != VendorBillVoid) {
		return "", fmt.Errorf("%s -> %s: %w", from, want, ErrBadStatus)
	}
	return want, nil
}

func (s *VendorBillStore) scan(row pgx.Row) (VendorBill, error) {
	var b VendorBill
	var bill time.Time
	var due *time.Time
	var status string
	if err := row.Scan(&b.ID, &b.Vendor, &bill, &due, &b.AmountCents, &b.TaxCents, &b.MatterID, &b.DocumentID, &status); err != nil {
		return VendorBill{}, err
	}
	b.BillDate = time.Date(bill.Year(), bill.Month(), bill.Day(), 0, 0, 0, 0, s.loc)
	if due != nil {
		d := time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, s.loc)
		b.DueDate = &d
	}
	b.Status = VendorBillStatus(status)
	return b, nil
}

// notFound maps a missing row or a malformed id to ErrNotFound.
func notFound(err error) error {
	var pg *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == pgInvalidText) {
		return ErrNotFound
	}
	return err
}

// Create implements VendorBills. A new bill is open.
func (s *VendorBillStore) Create(ctx context.Context, b VendorBill) (VendorBill, error) {
	if err := validateVendorBill(b); err != nil {
		return VendorBill{}, fmt.Errorf("create vendor bill: %w", err)
	}
	var due *string
	if b.DueDate != nil {
		d := civil(*b.DueDate, s.loc)
		due = &d
	}
	var out VendorBill
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.scan(tx.QueryRow(ctx, sqlInsertVendorBill, b.Vendor, civil(b.BillDate, s.loc), due, b.AmountCents, b.TaxCents, b.MatterID, b.DocumentID))
		return err
	})
	if err != nil {
		return VendorBill{}, fmt.Errorf("create vendor bill: %w", err)
	}
	return out, nil
}

// Get implements VendorBills.
func (s *VendorBillStore) Get(ctx context.Context, id string) (VendorBill, error) {
	var out VendorBill
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.scan(tx.QueryRow(ctx, sqlGetVendorBill, id))
		return notFound(err)
	})
	if err != nil {
		return VendorBill{}, fmt.Errorf("get vendor bill: %w", err)
	}
	return out, nil
}

// MarkPaid implements VendorBills: open -> paid.
func (s *VendorBillStore) MarkPaid(ctx context.Context, id string) (VendorBill, error) {
	return s.move(ctx, id, VendorBillPaid)
}

// Void implements VendorBills: open -> void.
func (s *VendorBillStore) Void(ctx context.Context, id string) (VendorBill, error) {
	return s.move(ctx, id, VendorBillVoid)
}

func (s *VendorBillStore) move(ctx context.Context, id string, want VendorBillStatus) (VendorBill, error) {
	var out VendorBill
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.scan(tx.QueryRow(ctx, sqlMoveVendorBill, id, string(want)))
		if !errors.Is(err, pgx.ErrNoRows) {
			return notFound(err)
		}
		// No open row moved: the bill is missing or already paid or void.
		cur, err := s.scan(tx.QueryRow(ctx, sqlGetVendorBill, id))
		if err != nil {
			return notFound(err)
		}
		_, err = nextStatus(cur.Status, want)
		return err
	})
	if err != nil {
		return VendorBill{}, fmt.Errorf("vendor bill %s: %w", want, err)
	}
	return out, nil
}
