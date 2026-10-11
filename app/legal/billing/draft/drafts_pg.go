package draft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/tax"
	"github.com/dagi377/counsel/kernel/money"
)

// ErrPlaceUnknown is returned when a draft's place of supply cannot be decided from the
// matter and client data. Drafting fails closed: it never assumes a province.
var ErrPlaceUnknown = errors.New("draft: place of supply unknown")

// A zero-rated supply to a non-resident (BIL-T-015) is an export. Its place_of_supply
// jurisdiction is PlaceExport and its place_of_supply_province is ExportProvince: the column is
// char(2) and 0001 requires it on an approved bill, so an export carries this marker, never NULL.
const (
	PlaceExport    = "export"
	ExportProvince = "EX"
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

// Places reads the place-of-supply facts of a matter's bill from the matter and its client:
// the client's address province and residency, the court province of commenced litigation
// and the province of the real property the matter relates to (tax.Supply).
type Places interface {
	Supply(ctx context.Context, tx pgx.Tx, matterID string) (tax.Supply, error)
}

// SQL PGDrafts runs. Tests script a pgx.Tx against these exact strings.
const (
	draftInsertSQL = `
INSERT INTO billing.bill (matter_id, payer_id, status, period, fees_cents, taxable_disb_cents, nontaxable_disb_cents,
                          tax_cents, total_cents, tax_breakdown, layout_id, created_by, updated_by,
                          place_of_supply_province, place_of_supply)
VALUES ($1::uuid, $2::uuid, 'draft', daterange($3::date, $4::date, '[]'), $5, $6, $7, $8, $9, $10::jsonb,
        nullif($11, '')::uuid, $12::uuid, $12::uuid, nullif($13, ''), $14::jsonb)
RETURNING id::text`
	draftLineInsertSQL = `
INSERT INTO billing.bill_line (bill_id, ordinal, source_type, time_entry_id, expense_id, narrative, quantity_milli,
                               rate_cents, amount_cents, tax_code)
VALUES ($1::uuid, $2, $3, nullif($4, '')::uuid, nullif($5, '')::uuid, $6, $7, $8, $9, $10)`
	draftLockSQL  = `SELECT status, discount_cents, matter_id::text FROM billing.bill WHERE id = $1::uuid FOR UPDATE`
	draftLinesSQL = `
SELECT ordinal, source_type, coalesce(time_entry_id::text, expense_id::text, ''), amount_cents, tax_code, on_hold
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
       tax_cents = $9, total_cents = $10, tax_breakdown = $11::jsonb, updated_by = $12::uuid,
       place_of_supply_province = nullif($13, ''), place_of_supply = $14::jsonb
 WHERE id = $1::uuid AND status IN ('draft', 'preflight_failed')`
	draftLineCodeSQL = `
UPDATE billing.bill_line SET tax_code = $2 WHERE bill_id = $1::uuid AND tax_code <> 'EXEMPT' AND tax_code <> $2`
)

// PGDrafts is the Postgres bill.Drafts over billing.bill and billing.bill_line. It reads and
// claims unbilled work through WIP and the payer through Payers. A draft's tax comes from
// the place-of-supply engine: Places gives the matter's supply facts, Decider applies the CRA
// place-of-supply rules (T-014.2) and Calc computes one line per component on fees and
// own-cost disbursements less the discount. There is no default province.
type PGDrafts struct {
	WIP     WIP
	Payers  Payers
	Places  Places
	Decider tax.PlaceOfSupply
	Calc    tax.Calculator
	Now     func() time.Time // the tax point of a draft; bills are dated in America/Toronto
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
	held     bool // on hold: on the draft, outside every total
}

// supply is a draft's decided place of supply and the date its rates are taken on.
type supply struct {
	place tax.Place
	on    time.Time
}

// province is the place_of_supply_province column: ExportProvince for a zero-rated supply.
func (s supply) province() string {
	if s.place.Province == tax.Zero {
		return ExportProvince
	}
	return s.place.Province
}

// jurisdiction is the place_of_supply jurisdiction: PlaceExport for a zero-rated supply.
func (s supply) jurisdiction() string {
	if s.place.Province == tax.Zero {
		return PlaceExport
	}
	return s.place.Province
}

// detail is the place_of_supply column: where the supply is made and the rules that decided it.
func (s supply) detail() (string, error) {
	b, err := json.Marshal(map[string]string{"jurisdiction": s.jurisdiction(), "rule_applied": strings.Join(s.place.Rules, ",")})
	return string(b), err
}

// lineCode is the tax code of a taxable line: the code of the place's first component.
func (s supply) lineCode() string {
	if s.place.Province == tax.Zero || len(s.place.Components) == 0 {
		return tax.Zero
	}
	return taxCode(s.place.Province, s.place.Components[0])
}

// taxCode is the billing.tax_rate code of component c at prov (tax.Seed).
func taxCode(prov string, c tax.Component) string {
	if c == tax.GST {
		return "GST"
	}
	return string(c) + "-" + prov
}

// place decides the place of supply of a draft on matterID, failing closed when it is unknown.
func (d PGDrafts) place(ctx context.Context, tx pgx.Tx, matterID string) (supply, error) {
	s, err := d.Places.Supply(ctx, tx, matterID)
	if err != nil {
		return supply{}, fmt.Errorf("place of supply of matter %s: %w", matterID, err)
	}
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		return supply{}, fmt.Errorf("place of supply: %w", err)
	}
	now := d.Now().In(loc)
	on := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	s.Date = on
	// rule: BIL-T-010
	p, err := d.Decider.Decide(ctx, s)
	if errors.Is(err, tax.ErrUnknownProvince) {
		return supply{}, fmt.Errorf("place of supply of matter %s: %w: %w", matterID, ErrPlaceUnknown, err)
	}
	if err != nil {
		return supply{}, fmt.Errorf("place of supply of matter %s: %w", matterID, err)
	}
	return supply{place: p, on: on}, nil
}

// compute returns the totals of lines under discount, taxed at the place of supply.
func (d PGDrafts) compute(ctx context.Context, lines []pgLine, discount int64, s supply) (totals, error) {
	var t totals
	for _, l := range lines {
		switch {
		case l.held:
			// A held line stays on the draft but is not billed.
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
	// rule: BIL-C-200
	taxLines, err := d.Calc.Compute(ctx, money.Cents(base), s.place, s.on)
	if err != nil {
		return totals{}, fmt.Errorf("draft totals: %w", err)
	}
	t.breakdown = []bill.TaxLine{}
	// rule: BIL-T-015
	// A zero-rated export keeps one line at a zero rate, so the bill shows why it has no tax.
	if s.place.Province == tax.Zero {
		t.breakdown = append(t.breakdown, bill.TaxLine{TaxCode: tax.Zero, Component: tax.Zero, BaseCents: base})
	}
	for _, l := range taxLines {
		t.tax += int64(l.Amount)
		t.breakdown = append(t.breakdown, bill.TaxLine{TaxCode: taxCode(s.place.Province, l.Component), Component: string(l.Component),
			RatePPM: int64(l.RatePPM), BaseCents: int64(l.Base), TaxCents: int64(l.Amount)})
	}
	t.total = t.fees + t.taxable + t.nontaxable - discount + t.tax
	return t, nil
}

func (d PGDrafts) ready() error {
	if d.WIP == nil || d.Payers == nil || d.Places == nil || d.Decider == nil || d.Calc == nil || d.Now == nil {
		return fmt.Errorf("drafts: WIP, Payers, Places, Decider, Calc and Now: %w", bill.ErrInvalid)
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
	// rule: BIL-T-010
	// Tax follows the place of supply; an unknown place refuses the draft before WIP is read.
	sup, err := d.place(ctx, tx, r.MatterID)
	if err != nil {
		return "", fmt.Errorf("create draft: %w", err)
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
		code := sup.lineCode()
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
	t, err := d.compute(ctx, lines, 0, sup)
	if err != nil {
		return "", fmt.Errorf("create draft: %w", err)
	}
	pos, err := sup.detail()
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
		t.fees, t.taxable, t.nontaxable, t.tax, t.total, string(taxes), r.TemplateID, r.PreparedBy, sup.province(), pos).Scan(&id); err != nil {
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
	var status, matterID string
	var discount int64
	if err := tx.QueryRow(ctx, draftLockSQL, string(id)).Scan(&status, &discount, &matterID); err != nil {
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
		err := r.Scan(&l.ordinal, &src, &l.sourceID, &l.amount, &l.taxCode, &l.held)
		l.source = bill.SourceType(src)
		return l, err
	})
	if err != nil {
		return fmt.Errorf("edit draft %s: %w", id, err)
	}
	for _, a := range e.LineActions {
		i := lineOf(lines, a.SourceID)
		if i < 0 || lines[i].held || a.Action == "hold" || a.Action == "write_off" {
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
	// rule: BIL-T-010
	// The place is decided again, so a draft follows the matter and client data it is edited under.
	sup, err := d.place(ctx, tx, matterID)
	if err != nil {
		return fmt.Errorf("edit draft %s: %w", id, err)
	}
	if _, err := tx.Exec(ctx, draftLineCodeSQL, string(id), sup.lineCode()); err != nil {
		return fmt.Errorf("edit draft %s: line tax codes: %w", id, err)
	}
	t, err := d.compute(ctx, lines, discount, sup)
	if err != nil {
		return fmt.Errorf("edit draft %s: %w", id, err)
	}
	pos, err := sup.detail()
	if err != nil {
		return fmt.Errorf("edit draft %s: %w", id, err)
	}
	taxes, err := json.Marshal(t.breakdown)
	if err != nil {
		return fmt.Errorf("edit draft %s: %w", id, err)
	}
	tag, err := tx.Exec(ctx, draftUpdateSQL, string(id), datePtr(e.PeriodFrom), datePtr(e.PeriodTo), e.TemplateID,
		t.fees, t.taxable, t.nontaxable, t.discount, t.tax, t.total, string(taxes), e.EditedBy, sup.province(), pos)
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
