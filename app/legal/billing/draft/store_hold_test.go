package draft

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/billing/bill"
)

// heldRow is one billing.bill_line row of the scripted draft.
type heldRow struct {
	id, source, entry, narrative string
	ordinal                      int
	gross                        int64 // amount before write-down and line discount
	reduction                    int64
	held                         bool
}

// heldTx is a scripted pgx.Tx over one draft bill: it answers the statements PGStore.SaveLines
// and PGDrafts.Edit run, by their exact SQL.
type heldTx struct {
	pgx.Tx
	status string
	rows   []heldRow
	fees   int64 // fees_cents of the last bill update
}

func (t *heldTx) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, t)
}

func (t *heldTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	switch sql {
	case storeLockSQL:
		return scripted{vals: []any{t.status, "11111111-1111-4111-8111-111111111111"}}
	case storeBillSQL:
		return scripted{vals: []any{t.status}}
	case draftLockSQL:
		return scripted{vals: []any{t.status, int64(0), "m"}}
	}
	return scripted{err: fmt.Errorf("unscripted query row %q", sql)}
}

func (t *heldTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	var out [][]any
	for _, r := range t.rows {
		switch sql {
		case storeLinesSQL:
			reason := ""
			if r.reduction > 0 {
				reason = string(ReasonCourtesy)
			}
			out = append(out, []any{r.id, r.ordinal, r.source, r.entry, r.narrative, r.gross, "HST-ON",
				r.reduction, reason, "", r.reduction > 0, int64(0), int64(0), int64(0), r.held})
		case draftLinesSQL:
			out = append(out, []any{r.ordinal, r.source, r.entry, r.gross - r.reduction, "HST-ON", r.held})
		default:
			return nil, fmt.Errorf("unscripted query %q", sql)
		}
	}
	return &scriptedRows{rows: out, i: -1}, nil
}

func (t *heldTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	switch sql {
	case storeLineUpdateSQL:
		for i := range t.rows {
			if t.rows[i].id == args[1] {
				t.rows[i].reduction = args[4].(int64)
				t.rows[i].held = args[11].(bool)
			}
		}
	case draftLineCodeSQL:
	case draftUpdateSQL:
		t.fees = args[4].(int64)
		return pgconn.NewCommandTag("UPDATE 1"), nil
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unscripted exec %q", sql)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type scripted struct {
	vals []any
	err  error
}

func (s scripted) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	return assign(s.vals, dest)
}

func assign(vals, dest []any) error {
	if len(vals) != len(dest) {
		return fmt.Errorf("scan: %d values into %d targets", len(vals), len(dest))
	}
	for i, d := range dest {
		reflect.ValueOf(d).Elem().Set(reflect.ValueOf(vals[i]))
	}
	return nil
}

type scriptedRows struct {
	pgx.Rows
	rows [][]any
	i    int
}

func (r *scriptedRows) Next() bool                    { r.i++; return r.i < len(r.rows) }
func (r *scriptedRows) Scan(dest ...any) error        { return assign(r.rows[r.i], dest) }
func (r *scriptedRows) Err() error                    { return nil }
func (r *scriptedRows) Close()                        {}
func (r *scriptedRows) CommandTag() pgconn.CommandTag { return pgconn.NewCommandTag("SELECT") }

// heldStore is a PGStore over tx with the Ontario place-of-supply engine.
func heldStore(tx *heldTx) PGStore {
	d := taxedDrafts(places{"m": {ClientProvince: "ON", RecipientResident: true}})
	d.WIP, d.Payers = noWIP{}, noPayers{}
	return PGStore{DB: tx, Drafts: d}
}

// TestSaveLinesHold: a hold is a status change. The held line is saved with on_hold set and
// stays on the draft; its amount leaves the bill's fees. Dropping a line is still refused,
// since bill lines are never deleted, and a held line can be released again.
//
// rule: BIL-C-021
func TestSaveLinesHold(t *testing.T) {
	ctx := context.Background()
	base := func() *heldTx {
		return &heldTx{status: string(bill.StatusDraft), rows: []heldRow{
			{id: "l1", source: "time", entry: "te1", narrative: "Review", ordinal: 1, gross: 50000},
			{id: "l2", source: "time", entry: "te2", narrative: "Call", ordinal: 2, gross: 20000},
		}}
	}
	tests := []struct {
		name     string
		tx       func() *heldTx
		edit     func(ls []Line) []Line
		want     error
		fees     int64
		wantHeld map[string]bool
	}{
		{"hold one line", base, func(ls []Line) []Line { ls[1].Held = true; return ls }, nil, 50000,
			map[string]bool{"l1": false, "l2": true}},
		{"release a held line", func() *heldTx { tx := base(); tx.rows[1].held = true; return tx },
			func(ls []Line) []Line { ls[1].Held = false; return ls }, nil, 70000, map[string]bool{"l1": false, "l2": false}},
		{"a dropped line is refused", base, func(ls []Line) []Line { return ls[:1] }, bill.ErrInvalid, 0, nil},
		{"past draft is refused", func() *heldTx { tx := base(); tx.status = "approved"; return tx },
			func(ls []Line) []Line { ls[0].Held = true; return ls }, ErrNotDraft, 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := tc.tx()
			s := heldStore(tx)
			lines, err := s.Lines(ctx, "b1")
			if err != nil {
				t.Fatalf("lines: %v", err)
			}
			err = s.SaveLines(ctx, "b1", tc.edit(lines))
			if !errors.Is(err, tc.want) {
				t.Fatalf("save = %v, want %v", err, tc.want)
			}
			if err != nil {
				return
			}
			if tx.fees != tc.fees {
				t.Fatalf("fees = %d, want %d", tx.fees, tc.fees)
			}
			got, err := s.Lines(ctx, "b1")
			if err != nil || len(got) != len(tx.rows) {
				t.Fatalf("lines after save = %v, %v", got, err)
			}
			for _, l := range got {
				if l.Held != tc.wantHeld[l.ID] {
					t.Fatalf("line %s held = %v, want %v", l.ID, l.Held, tc.wantHeld[l.ID])
				}
			}
		})
	}
}

// A held line counts toward no total or tax of its draft.
//
// rule: BIL-C-021
func TestHeldLineOutsideTotals(t *testing.T) {
	d := taxedDrafts(places{"m": {ClientProvince: "ON", RecipientResident: true}})
	sup, err := d.place(context.Background(), nil, "m")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		lines      []pgLine
		fees, disb int64
	}{
		{"nothing held", []pgLine{{source: bill.SourceTime, amount: 1000}, {source: bill.SourceExpense, amount: 500}}, 1000, 500},
		{"held fee", []pgLine{{source: bill.SourceTime, amount: 1000, held: true}, {source: bill.SourceExpense, amount: 500}}, 0, 500},
		{"held disbursement", []pgLine{{source: bill.SourceTime, amount: 1000}, {source: bill.SourceExpense, amount: 500, held: true}}, 1000, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := d.compute(context.Background(), tc.lines, 0, sup)
			if err != nil {
				t.Fatal(err)
			}
			if got.fees != tc.fees || got.taxable != tc.disb {
				t.Fatalf("totals = %+v, want fees %d disbursements %d", got, tc.fees, tc.disb)
			}
		})
	}
}
