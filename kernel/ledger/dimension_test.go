package ledger

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// testDims stands for the optional journal_line columns: kernel ones plus an app's link
// column, which the kernel only ever sees as a name.
var testDims = []dimension{{"party", "text"}, {"tax_code", "text"}, {"bank_line_id", "uuid"}, {"link_id", "uuid"}}

// fakeTx answers the ledger's statements from canned rows and records every Exec.
// Methods the ledger does not call panic through the nil embedded Tx.
type fakeTx struct {
	pgx.Tx
	closed  bool                // sqlPeriodClosed answer
	dims    []dimension         // sqlLineDimensions answer
	lines   [][]any             // selectLinesSQL answer, one row per line
	execs   []execCall          // recorded Exec calls
	queries map[string]struct{} // statements seen
}

type execCall struct {
	sql  string
	args []any
}

func (f *fakeTx) seen(sql string) {
	if f.queries == nil {
		f.queries = map[string]struct{}{}
	}
	f.queries[sql] = struct{}{}
}

func (f *fakeTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	f.seen(sql)
	switch {
	case sql == sqlFindBySource || sql == sqlReversalOf:
		return fakeRow{err: pgx.ErrNoRows}
	case sql == sqlPeriodClosed:
		return fakeRow{vals: []any{f.closed}}
	case sql == sqlEntryExists:
		return fakeRow{vals: []any{true}}
	case sql == sqlAccountID:
		return fakeRow{vals: []any{"acct-id"}}
	case sql == sqlInsertEntry || sql == sqlInsertReversal:
		return fakeRow{vals: []any{"new-entry"}}
	}
	return fakeRow{err: fmt.Errorf("fakeTx: unexpected QueryRow %q", sql)}
}

func (f *fakeTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	f.seen(sql)
	switch {
	case sql == sqlLineDimensions:
		var rows [][]any
		for _, d := range f.dims {
			rows = append(rows, []any{d.Name, d.Type})
		}
		return &fakeRows{rows: rows}, nil
	case strings.HasPrefix(sql, "SELECT account_id::text"):
		return &fakeRows{rows: f.lines}, nil
	}
	return nil, fmt.Errorf("fakeTx: unexpected Query %q", sql)
}

func (f *fakeTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.seen(sql)
	f.execs = append(f.execs, execCall{sql: sql, args: args})
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type fakeRow struct {
	vals []any
	err  error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return assign(r.vals, dest)
}

type fakeRows struct {
	pgx.Rows
	rows [][]any
	i    int
}

func (r *fakeRows) Next() bool             { r.i++; return r.i <= len(r.rows) }
func (r *fakeRows) Scan(dest ...any) error { return assign(r.rows[r.i-1], dest) }
func (r *fakeRows) Err() error             { return nil }
func (r *fakeRows) Close()                 {}

func assign(vals, dest []any) error {
	if len(vals) != len(dest) {
		return fmt.Errorf("scan: %d values into %d targets", len(vals), len(dest))
	}
	for i, v := range vals {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

func poster(context.Context) (string, error) { return "u1", nil }

// rule: BIL-I-025
func TestBIL_I_025_PostIntoClosedPeriodIsErrPeriodClosed(t *testing.T) {
	e := Entry{Book: "main", Date: Date{Year: 2026, Month: time.March, Day: 15}, SourceKind: "manual", SourceID: "s1",
		Lines: []Line{line("1000", Debit, 500), line("4000", Credit, 500)}}
	tests := []struct {
		name   string
		closed bool
		want   error
	}{
		{"closed period refused", true, ErrPeriodClosed},
		{"open period posts", false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeTx{closed: tt.closed}
			g := &PG{PostedBy: poster}
			_, err := g.Post(context.Background(), tx, e)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Post = %v, want %v", err, tt.want)
			}
			if _, ok := tx.queries[sqlPeriodClosed]; !ok {
				t.Fatal("Post did not check the period")
			}
			if wrote := len(tx.execs) > 0 || has(tx.queries, sqlInsertEntry); wrote == tt.closed {
				t.Fatalf("wrote = %v with closed = %v", wrote, tt.closed)
			}
		})
	}
}

func has(m map[string]struct{}, k string) bool { _, ok := m[k]; return ok }

// rule: BIL-I-019
func TestBIL_I_019_PostWritesLineDimensions(t *testing.T) {
	d := Date{Year: 2026, Month: time.April, Day: 2}
	tests := []struct {
		name     string
		lines    []Line
		wantErr  error
		wantArgs [][]any // dims args per line, testDims order
	}{
		{name: "no dimensions uses the core insert", lines: []Line{line("1000", Debit, 5), line("4000", Credit, 5)}},
		{name: "app link and party written", lines: []Line{
			{Account: "1000", Side: Debit, Amount: 5, Dimensions: map[string]string{"link_id": "m1", "party": "p"}},
			{Account: "4000", Side: Credit, Amount: 5, Dimensions: map[string]string{"link_id": "m1", "tax_code": ""}},
		}, wantArgs: [][]any{{"p", nil, nil, "m1"}, {nil, nil, nil, "m1"}}},
		{name: "unknown dimension refused", lines: []Line{
			{Account: "1000", Side: Debit, Amount: 5, Dimensions: map[string]string{"nope": "x"}}, line("4000", Credit, 5),
		}, wantErr: ErrInvalidEntry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeTx{dims: testDims}
			_, err := (&PG{PostedBy: poster}).Post(context.Background(), tx, Entry{Book: "main", Date: d, SourceKind: "manual", SourceID: "s", Lines: tt.lines})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Post = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				if len(tx.execs) != 0 {
					t.Fatalf("refused entry wrote %d lines", len(tx.execs))
				}
				return
			}
			if len(tx.execs) != len(tt.lines) {
				t.Fatalf("execs = %d, want %d", len(tx.execs), len(tt.lines))
			}
			for i, x := range tx.execs {
				if tt.wantArgs == nil {
					if x.sql != sqlInsertLine {
						t.Fatalf("line %d sql = %s, want core insert", i, x.sql)
					}
					continue
				}
				if x.sql != insertLineSQL(testDims) {
					t.Fatalf("line %d sql = %s", i, x.sql)
				}
				if got := x.args[4:]; !reflect.DeepEqual(got, tt.wantArgs[i]) {
					t.Fatalf("line %d dims = %v, want %v", i, got, tt.wantArgs[i])
				}
			}
		})
	}
}

// Reverse copies every optional journal_line column, an app's link column included, with
// debit and credit swapped.
// rule: BIL-I-019
func TestBIL_I_019_ReverseCopiesEveryDimension(t *testing.T) {
	tx := &fakeTx{
		dims: testDims,
		lines: [][]any{
			{"acc-a", int64(500), int64(0), strp("p"), (*string)(nil), strp("bl"), strp("m1")},
			{"acc-b", int64(0), int64(500), (*string)(nil), strp("t"), (*string)(nil), strp("m1")},
		},
	}
	if _, err := (&PG{PostedBy: poster}).Reverse(context.Background(), tx, "e1", "wrong account"); err != nil {
		t.Fatal(err)
	}
	want := []execCall{
		{insertLineSQL(testDims), []any{"new-entry", "acc-a", int64(0), int64(500), "p", nil, "bl", "m1"}},
		{insertLineSQL(testDims), []any{"new-entry", "acc-b", int64(500), int64(0), nil, "t", nil, "m1"}},
	}
	if !reflect.DeepEqual(tx.execs, want) {
		t.Fatalf("reversal lines =\n %v\nwant\n %v", tx.execs, want)
	}
}

func TestLineSQLQuotesAndCastsDimensions(t *testing.T) {
	dims := []dimension{{"link_id", "uuid"}, {`we"ird`, "text"}}
	tests := []struct {
		name, got, want string
	}{
		{"insert", insertLineSQL(dims), "INSERT INTO gl.journal_line (entry_id, account_id, debit_cents, credit_cents, \"link_id\", \"we\"\"ird\")\nVALUES ($1::uuid, $2::uuid, $3, $4, $5::text::uuid, $6::text::text)"},
		{"select", selectLinesSQL(dims), "SELECT account_id::text, debit_cents, credit_cents, \"link_id\"::text, \"we\"\"ird\"::text\nFROM gl.journal_line WHERE entry_id = $1::uuid ORDER BY id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("sql =\n %s\nwant\n %s", tt.got, tt.want)
			}
		})
	}
}
