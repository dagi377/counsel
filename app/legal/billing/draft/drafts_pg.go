package draft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/kernel/money"
)

// WIPItem is one unbilled time entry or expense a draft can bill. Amounts are int64 cents.
type WIPItem struct {
	Source        bill.SourceType // SourceTime or SourceExpense
	SourceID      string
	Date          time.Time
	Narrative     string
	QuantityMilli int64
	RateCents     int64
	AmountCents   int64
	Agency        bool // an agent disbursement: outside the tax base
}

// WIP is the timekeeping side of drafting: it reads a matter's unbilled work and claims the
// items a draft bills. Timekeeping owns those rows; billing never writes them.
type WIP interface {
	// Unbilled returns the matter's approved, unbilled items dated in [from, to]; a zero
	// from means from the oldest one.
	Unbilled(ctx context.Context, tx pgx.Tx, matterID string, from, to time.Time) ([]WIPItem, error)
	// Claim puts the items on draft bill billID.
	Claim(ctx context.Context, tx pgx.Tx, billID string, items []WIPItem) error
}

// Payers names who a matter's bills go to (the matter's client).
type Payers interface {
	Payer(ctx context.Context, tx pgx.Tx, matterID string) (string, error)
}

// SQL PGDrafts runs. Tests script a pgx.Tx against these exact strings.
const (
	draftInsertSQL = `
INSERT INTO billing.bill (matter_id, payer_id, status, period, fees_cents, taxable_disb_cents, nontaxable_disb_cents,
                          tax_cents, total_cents, tax_breakdown, layout_id, created_by, updated_by)
VALUES ($1::uuid, $2::uuid, 'draft', daterange($3::date, $4::date, '[]'), $5, $6, $7, $8, $9, $10::jsonb,
        nullif($11, '')::uuid, $12::uuid, $12::uuid)
RETURNING id::text`
	draftLineInsertSQL = `
INSERT INTO billing.bill_line (bill_id, ordinal, source_type, time_entry_id, expense_id, narrative, quantity_milli,
                               rate_cents, amount_cents, tax_code)
VALUES ($1::uuid, $2, $3, nullif($4, '')::uuid, nullif($5, '')::uuid, $6, $7, $8, $9, $10)`
	draftLockSQL  = `SELECT status, discount_cents FROM billing.bill WHERE id = $1::uuid FOR UPDATE`
	draftLinesSQL = `
SELECT ordinal, source_type, coalesce(time_entry_id::text, expense_id::text, ''), amount_cents, tax_code
  FROM billing.bill_line WHERE bill_id = $1::uuid ORDER BY ordinal`
	draftLineUpdateSQL = `
UPDATE billing.bill_line SET amount_cents = $3, narrative = coalesce(nullif($4, ''), narrative)
 WHERE bill_id = $1::uuid AND ordinal = $2`
	draftUpdateSQL = `
UPDATE billing.bill
   SET period = CASE WHEN $2::date IS NULL AND $3::date IS NULL THEN period
                     ELSE daterange(coalesce($2::date, lower(period)), coalesce($3::date, upper(period) - 1), '[]') END,
       layout_id = CASE WHEN $4::text IS NULL THEN layout_id ELSE nullif($4, '')::uuid END,
       fees_cents = $5, taxable_disb_cents = $6, nontaxable_disb_cents = $7, discount_cents = $8,
       tax_cents = $9, total_cents = $10, tax_breakdown = $11::jsonb, updated_by = $12::uuid
 WHERE id = $1::uuid AND status IN ('draft', 'preflight_failed')`
)

// PGDrafts is the Postgres bill.Drafts over billing.bill and billing.bill_line. It reads and
// claims unbilled work through WIP and the payer through Payers. A draft's tax is one
// component (TaxCode at TaxRatePPM) on fees and own-cost disbursements less the discount.
type PGDrafts struct {
	WIP        WIP
	Payers     Payers
	TaxCode    string // e.g. "HST-ON"
	Component  string // e.g. "HST"
	TaxRatePPM int64  // e.g. 130000
}

var _ bill.Drafts = PGDrafts{}

// totals are a draft's stored amounts, all int64 cents.
type totals struct {
	fees, taxable, nontaxable, discount, tax, total int64
	breakdown                                       []bill.TaxLine
}

// line is a draft bill line as totals sees it.
type pgLine struct {
	ordinal  int
	source   bill.SourceType
	sourceID string
	amount   int64
	taxCode  string
}

// compute returns the totals of lines under discount. It has no I/O.
func (d PGDrafts) compute(lines []pgLine, discount int64) (totals, error) {
	var t totals
	for _, l := range lines {
		switch {
		case l.source == bill.SourceExpense && l.taxCode == bill.TaxCodeExempt:
			t.nontaxable += l.amount
		case l.source == bill.SourceExpense:
			t.taxable += l.amount
		default:
			t.fees += l.amount
		}
	}
	// rule: BIL-C-021
	if discount < 0 || discount > t.fees+t.taxable || t.fees < 0 {
		return totals{}, fmt.Errorf("draft totals: discount: %w", bill.ErrInvalid)
	}
	t.discount = discount
	base := t.fees + t.taxable - discount
	tax, err := money.Rhu(base*d.TaxRatePPM, 1_000_000)
	if err != nil {
		return totals{}, fmt.Errorf("draft totals: %w", err)
	}
	t.tax = tax
	t.total = t.fees + t.taxable + t.nontaxable - discount + tax
	t.breakdown = []bill.TaxLine{{TaxCode: d.TaxCode, Component: d.Component, RatePPM: d.TaxRatePPM, BaseCents: base, TaxCents: tax}}
	return t, nil
}

func (d PGDrafts) ready() error {
	if d.WIP == nil || d.Payers == nil || d.TaxCode == "" || d.Component == "" || d.TaxRatePPM < 0 {
		return fmt.Errorf("drafts: WIP, Payers and tax: %w", bill.ErrInvalid)
	}
	return nil
}

// Create drafts a bill from the matter's unbilled WIP after the line actions, or returns
// bill.ErrNoUnbilledWIP when nothing is left to bill.
func (d PGDrafts) Create(ctx context.Context, tx pgx.Tx, r bill.DraftRequest) (bill.ID, error) {
	if err := d.ready(); err != nil {
		return "", err
	}
	if r.MatterID == "" || r.PreparedBy == "" || r.PeriodTo.IsZero() {
		return "", fmt.Errorf("create draft: %w", bill.ErrInvalid)
	}
	items, err := d.WIP.Unbilled(ctx, tx, r.MatterID, r.PeriodFrom, r.PeriodTo)
	if err != nil {
		return "", fmt.Errorf("create draft: unbilled: %w", err)
	}
	actions := map[string]bill.LineAction{}
	for _, a := range r.LineActions {
		actions[a.SourceID] = a
	}
	var lines []pgLine
	var billed []WIPItem
	from := r.PeriodFrom
	for _, it := range items {
		a, ok := actions[it.SourceID]
		if !ok {
			a.Action = "bill"
		}
		amount, keep, err := applyAction(it.AmountCents, a)
		if err != nil {
			return "", fmt.Errorf("create draft: %w", err)
		}
		if !keep {
			continue
		}
		if a.Narrative != "" {
			it.Narrative = a.Narrative
		}
		it.AmountCents = amount
		code := d.TaxCode
		if it.Source == bill.SourceExpense && it.Agency {
			code = bill.TaxCodeExempt
		}
		lines = append(lines, pgLine{ordinal: len(lines) + 1, source: it.Source, sourceID: it.SourceID, amount: amount, taxCode: code})
		billed = append(billed, it)
		if from.IsZero() || it.Date.Before(from) {
			from = it.Date
		}
	}
	// rule: BIL-C-020
	if len(billed) == 0 {
		return "", fmt.Errorf("create draft for matter: %w", bill.ErrNoUnbilledWIP)
	}
	t, err := d.compute(lines, 0)
	if err != nil {
		return "", fmt.Errorf("create draft: %w", err)
	}
	payer, err := d.Payers.Payer(ctx, tx, r.MatterID)
	if err != nil {
		return "", fmt.Errorf("create draft: payer: %w", err)
	}
	taxes, err := json.Marshal(t.breakdown)
	if err != nil {
		return "", fmt.Errorf("create draft: %w", err)
	}
	var id string
	if err := tx.QueryRow(ctx, draftInsertSQL, r.MatterID, payer, from.Format(time.DateOnly), r.PeriodTo.Format(time.DateOnly),
		t.fees, t.taxable, t.nontaxable, t.tax, t.total, string(taxes), r.TemplateID, r.PreparedBy).Scan(&id); err != nil {
		return "", fmt.Errorf("create draft: %w", err)
	}
	for i, l := range lines {
		it := billed[i]
		var timeID, expenseID string
		if l.source == bill.SourceTime {
			timeID = l.sourceID
		} else {
			expenseID = l.sourceID
		}
		if _, err := tx.Exec(ctx, draftLineInsertSQL, id, l.ordinal, string(l.source), timeID, expenseID, it.Narrative,
			it.QuantityMilli, it.RateCents, l.amount, l.taxCode); err != nil {
			return "", fmt.Errorf("create draft %s line %d: %w", id, l.ordinal, err)
		}
	}
	if err := d.WIP.Claim(ctx, tx, id, billed); err != nil {
		return "", fmt.Errorf("create draft %s: claim: %w", id, err)
	}
	return bill.ID(id), nil
}

// applyAction returns the line amount after a, and whether the item stays on the bill. A
// held or written-off item stays unbilled; no charge bills it at zero.
func applyAction(amount int64, a bill.LineAction) (int64, bool, error) {
	switch a.Action {
	case "bill":
		return amount, true, nil
	case "hold", "write_off":
		return 0, false, nil
	case "no_charge":
		return 0, true, nil
	case "write_down":
		// rule: BIL-C-023
		if a.ReasonCode == "" {
			return 0, false, fmt.Errorf("write-down: %w: %w", ErrReasonRequired, bill.ErrInvalid)
		}
		if a.WriteDownCents < 0 || a.WriteDownCents > amount {
			return 0, false, fmt.Errorf("write-down: %w: %w", ErrNegativeLine, bill.ErrInvalid)
		}
		return amount - a.WriteDownCents, true, nil
	}
	return 0, false, fmt.Errorf("line action %q: %w", a.Action, bill.ErrInvalid)
}

// Edit changes a draft and recomputes its totals. A bill past draft is bill.ErrNotEditable.
// Line actions on a draft's lines may bill, write down or charge nothing; an item comes off
// a draft only by voiding it, since bill lines are never deleted.
func (d PGDrafts) Edit(ctx context.Context, tx pgx.Tx, id bill.ID, e bill.DraftEdit) error {
	if err := d.ready(); err != nil {
		return err
	}
	if e.EditedBy == "" {
		return fmt.Errorf("edit draft %s: %w", id, bill.ErrInvalid)
	}
	var status string
	var discount int64
	if err := tx.QueryRow(ctx, draftLockSQL, string(id)).Scan(&status, &discount); err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
			return fmt.Errorf("edit draft %s: %w", id, bill.ErrNotFound)
		}
		return fmt.Errorf("edit draft %s: %w", id, err)
	}
	if status != string(bill.StatusDraft) && status != string(bill.StatusPreflightFailed) {
		return fmt.Errorf("edit draft %s in %s: %w", id, status, bill.ErrNotEditable)
	}
	rows, err := tx.Query(ctx, draftLinesSQL, string(id))
	if err != nil {
		return fmt.Errorf("edit draft %s: %w", id, err)
	}
	lines, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pgLine, error) {
		var l pgLine
		var src string
		err := r.Scan(&l.ordinal, &src, &l.sourceID, &l.amount, &l.taxCode)
		l.source = bill.SourceType(src)
		return l, err
	})
	if err != nil {
		return fmt.Errorf("edit draft %s: %w", id, err)
	}
	for _, a := range e.LineActions {
		i := lineOf(lines, a.SourceID)
		if i < 0 || a.Action == "hold" || a.Action == "write_off" {
			return fmt.Errorf("edit draft %s: line action %q: %w", id, a.Action, bill.ErrInvalid)
		}
		amount, _, err := applyAction(lines[i].amount, a)
		if err != nil {
			return fmt.Errorf("edit draft %s: %w", id, err)
		}
		if _, err := tx.Exec(ctx, draftLineUpdateSQL, string(id), lines[i].ordinal, amount, a.Narrative); err != nil {
			return fmt.Errorf("edit draft %s line %d: %w", id, lines[i].ordinal, err)
		}
		lines[i].amount = amount
	}
	if e.DiscountCents != nil {
		discount = *e.DiscountCents
	}
	t, err := d.compute(lines, discount)
	if err != nil {
		return fmt.Errorf("edit draft %s: %w", id, err)
	}
	taxes, err := json.Marshal(t.breakdown)
	if err != nil {
		return fmt.Errorf("edit draft %s: %w", id, err)
	}
	tag, err := tx.Exec(ctx, draftUpdateSQL, string(id), datePtr(e.PeriodFrom), datePtr(e.PeriodTo), e.TemplateID,
		t.fees, t.taxable, t.nontaxable, t.discount, t.tax, t.total, string(taxes), e.EditedBy)
	if err != nil {
		return fmt.Errorf("edit draft %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("edit draft %s: %w", id, bill.ErrNotEditable)
	}
	return nil
}

func lineOf(lines []pgLine, sourceID string) int {
	for i, l := range lines {
		if l.sourceID != "" && l.sourceID == sourceID {
			return i
		}
	}
	return -1
}

func datePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
}
