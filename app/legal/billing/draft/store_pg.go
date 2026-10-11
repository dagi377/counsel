package draft

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/billing/batch"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/kernel/clock"
)

// SQL PGStore and PGBatch run.
const (
	storeLockSQL = `SELECT status, coalesce(created_by::text, '') FROM billing.bill WHERE id = $1::uuid FOR UPDATE`
	storeBillSQL = `SELECT status FROM billing.bill WHERE id = $1::uuid`
	// amount_cents is the line's billed net before the bill discount: the gross less its
	// write-down and line discount. The bill discount is held on billing.bill.
	storeLinesSQL = `
SELECT id::text, ordinal, source_type, coalesce(time_entry_id::text, ''), narrative,
       amount_cents + reduction_cents + line_discount_cents, tax_code, reduction_cents,
       coalesce(reduction_reason, ''), coalesce(reduction_text, ''), reduction_shown,
       markup_cents, line_discount_cents, discount_share_cents
  FROM billing.bill_line WHERE bill_id = $1::uuid ORDER BY ordinal`
	storeLineUpdateSQL = `
UPDATE billing.bill_line
   SET narrative = coalesce(nullif($3, ''), narrative), amount_cents = $4, reduction_cents = $5,
       reduction_reason = nullif($6, ''), reduction_text = nullif($7, ''), reduction_shown = $8,
       markup_cents = $9, line_discount_cents = $10, discount_share_cents = $11
 WHERE bill_id = $1::uuid AND id = $2::uuid`
	batchKeySQL    = `SELECT id::text FROM billing.bill WHERE idempotency_key = $1`
	batchKeySetSQL = `UPDATE billing.bill SET idempotency_key = $2 WHERE id = $1::uuid`
)

// taxExempt is the line tax code of an agent disbursement (no HST, BIL-C-041).
const taxExempt = "EXEMPT"

// PGStore is the Postgres Store over billing.bill_line. SaveLines changes a draft's lines in
// place and recomputes the bill's totals and tax through Drafts, in one transaction.
type PGStore struct {
	DB     bill.TxRunner
	Drafts PGDrafts
}

var _ Store = PGStore{}

// storedLine is a bill line as stored, with its gross amount.
type storedLine struct {
	Line
	source string
}

func lineKind(source, taxCode string) LineKind {
	switch source {
	case "flat":
		return LineFlat
	case "expense":
		if taxCode == taxExempt {
			return LineAgentDis
		}
		return LineOwnCost
	}
	return LineFee
}

func readLines(ctx context.Context, tx pgx.Tx, billID string) ([]storedLine, error) {
	rows, err := tx.Query(ctx, storeLinesSQL, billID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (storedLine, error) {
		var (
			s                  storedLine
			taxCode, reason    string
			reasonText         string
			reductionCents     int64
			shown              bool
			l                  = &s.Line
			markup, lineDisc   int64
			share              int64
			sourceEntry, descr string
		)
		err := r.Scan(&l.ID, &l.SortOrder, &s.source, &sourceEntry, &descr, &l.AmountCents, &taxCode,
			&reductionCents, &reason, &reasonText, &shown, &markup, &lineDisc, &share)
		l.Kind = lineKind(s.source, taxCode)
		l.Description = descr
		l.SourceTimeEntryID = sourceEntry
		l.Taxable = taxCode != taxExempt
		l.MarkupCents, l.LineDiscountCents, l.DiscountShareCents = markup, lineDisc, share
		if reason != "" {
			l.Reduction = &Reduction{Cents: reductionCents, Reason: Reason(reason), ReasonText: reasonText, ShowOnBill: shown}
		}
		return s, err
	})
}

func notFound(err error) bool {
	var pg *pgconn.PgError
	return errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02")
}

// Lines implements Store: the draft's lines in ordinal order. An unknown bill is ErrNotFound.
func (s PGStore) Lines(ctx context.Context, billID string) ([]Line, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("lines %s: no database: %w", billID, ErrNotFound)
	}
	var out []Line
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, storeBillSQL, billID).Scan(&status); err != nil {
			return err
		}
		stored, err := readLines(ctx, tx, billID)
		for _, l := range stored {
			out = append(out, l.Line)
		}
		return err
	})
	if notFound(err) {
		return nil, fmt.Errorf("lines %s: %w", billID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("lines %s: %w", billID, err)
	}
	return out, nil
}

// SaveLines implements Store. Each line is matched by id to a stored line of the draft; its
// description, write-down, markup and discounts are saved and the bill's totals recomputed.
// A bill past draft is ErrNotDraft. The gross amount comes from the billed work and is not
// changed here, and bill lines are never deleted: an item comes off a draft only by voiding
// it, so a stored line missing from lines, an unknown line or a changed gross is
// bill.ErrInvalid.
func (s PGStore) SaveLines(ctx context.Context, billID string, lines []Line) error {
	if s.DB == nil {
		return fmt.Errorf("save lines %s: no database: %w", billID, ErrNotFound)
	}
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var status, preparer string
		if err := tx.QueryRow(ctx, storeLockSQL, billID).Scan(&status, &preparer); err != nil {
			if notFound(err) {
				return ErrNotFound
			}
			return err
		}
		if status != string(bill.StatusDraft) && status != string(bill.StatusPreflightFailed) {
			return fmt.Errorf("bill in %s: %w", status, ErrNotDraft)
		}
		stored, err := readLines(ctx, tx, billID)
		if err != nil {
			return err
		}
		byID := make(map[string]storedLine, len(stored))
		for _, l := range stored {
			byID[l.ID] = l
		}
		seen := make(map[string]bool, len(lines))
		for _, l := range lines {
			cur, ok := byID[l.ID]
			if !ok || seen[l.ID] {
				return fmt.Errorf("line %q: not a line of this draft: %w", l.ID, bill.ErrInvalid)
			}
			seen[l.ID] = true
			if l.AmountCents != cur.AmountCents {
				return fmt.Errorf("line %s: gross amount is not changed here: %w", l.ID, bill.ErrInvalid)
			}
			if err := checkSaved(l); err != nil {
				return fmt.Errorf("line %s: %w", l.ID, err)
			}
			var red Reduction
			if l.Reduction != nil {
				red = *l.Reduction
			}
			net := l.AmountCents - red.Cents - l.LineDiscountCents
			if _, err := tx.Exec(ctx, storeLineUpdateSQL, billID, l.ID, l.Description, net, red.Cents,
				string(red.Reason), red.ReasonText, red.ShowOnBill, l.MarkupCents, l.LineDiscountCents,
				l.DiscountShareCents); err != nil {
				return fmt.Errorf("line %s: %w", l.ID, err)
			}
		}
		if len(seen) != len(stored) {
			return fmt.Errorf("a line comes off a draft only by voiding it: %w", bill.ErrInvalid)
		}
		if preparer == "" {
			return fmt.Errorf("draft has no preparer: %w", bill.ErrInvalid)
		}
		// Totals, discount and tax are recomputed from the saved line amounts.
		return s.Drafts.Edit(ctx, tx, bill.ID(billID), bill.DraftEdit{EditedBy: preparer})
	})
	if err != nil {
		return fmt.Errorf("save lines %s: %w", billID, err)
	}
	return nil
}

// checkSaved checks a line's write-down and amounts before it is stored.
func checkSaved(l Line) error {
	if l.Reduction != nil {
		// rule: BIL-C-023
		if _, err := ApplyReduction(Line{ID: l.ID, AmountCents: l.AmountCents}, *l.Reduction); err != nil {
			return err
		}
	}
	if l.MarkupCents < 0 || l.LineDiscountCents < 0 || l.DiscountShareCents < 0 {
		return fmt.Errorf("negative markup or discount: %w", ErrNegativeLine)
	}
	// rule: BIL-C-021
	if LineNet(l) < 0 {
		return ErrNegativeLine
	}
	return nil
}

// PGBatch is the Postgres batch.Drafts: Create builds one draft through Drafts in its own
// transaction and stores it under the key on billing.bill.idempotency_key, so the same key
// returns the first draft. PreparedBy names the user the draft is prepared by (the bill's
// created_by); it never approves.
type PGBatch struct {
	DB         bill.TxRunner
	Drafts     PGDrafts
	PreparedBy func(ctx context.Context, t batch.Target) (string, error)
	Loc        *time.Location // business zone of periodTo; nil is America/Toronto
}

var _ batch.Drafts = PGBatch{}

func (b PGBatch) existing(ctx context.Context, key string) (string, bool, error) {
	var id string
	err := b.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, batchKeySQL, key).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

// Create implements batch.Drafts.
//
// rule: BIL-C-110
func (b PGBatch) Create(ctx context.Context, key string, t batch.Target, periodTo clock.Date) (string, bool, error) {
	if b.DB == nil || b.PreparedBy == nil {
		return "", false, fmt.Errorf("batch draft: database and preparer: %w", bill.ErrInvalid)
	}
	if key == "" || t.MatterID == "" {
		// Client-level drafts are built per matter by the caller.
		return "", false, fmt.Errorf("batch draft: key and matter: %w", batch.ErrInvalid)
	}
	if id, ok, err := b.existing(ctx, key); err != nil || ok {
		if err != nil {
			return "", false, fmt.Errorf("batch draft %s: %w", t.MatterID, err)
		}
		return id, false, nil
	}
	preparer, err := b.PreparedBy(ctx, t)
	if err != nil {
		return "", false, fmt.Errorf("batch draft %s: preparer: %w", t.MatterID, err)
	}
	loc := b.Loc
	if loc == nil {
		if loc, err = time.LoadLocation("America/Toronto"); err != nil {
			return "", false, fmt.Errorf("batch draft: %w", err)
		}
	}
	var id string
	err = b.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		bid, err := b.Drafts.Create(ctx, tx, bill.DraftRequest{MatterID: t.MatterID, PeriodTo: periodTo.Start(loc), PreparedBy: preparer})
		if err != nil {
			return err
		}
		id = string(bid)
		_, err = tx.Exec(ctx, batchKeySetSQL, id, key)
		return err
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		// A concurrent call stored the key first: its draft is the one.
		if id, ok, rerr := b.existing(ctx, key); rerr == nil && ok {
			return id, false, nil
		}
	}
	if err != nil {
		return "", false, fmt.Errorf("batch draft %s: %w", t.MatterID, err)
	}
	return id, true, nil
}
