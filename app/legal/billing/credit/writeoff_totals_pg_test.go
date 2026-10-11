package credit

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

// totalsRow scans one scripted row.
type totalsRow struct {
	vals []any
	err  error
}

func (r totalsRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(r.vals) != len(dest) {
		return fmt.Errorf("scan %d values into %d", len(r.vals), len(dest))
	}
	for i, v := range r.vals {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

// totalsRows is a scripted pgx.Rows.
type totalsRows struct {
	pgx.Rows
	rows [][]any
	i    int
}

func (r *totalsRows) Next() bool             { r.i++; return r.i <= len(r.rows) }
func (r *totalsRows) Scan(dest ...any) error { return totalsRow{vals: r.rows[r.i-1]}.Scan(dest...) }
func (r *totalsRows) Err() error             { return nil }
func (r *totalsRows) Close()                 {}

// totalsTx answers the four statements from scripted rows and records the order and args.
type totalsTx struct {
	pgx.Tx
	lock     totalsRow
	bills    [][]any
	approved [][]any
	pending  [][]any
	queryErr map[string]error
	calls    []string
	args     map[string][]any
}

func (t *totalsTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	t.record(sql, args)
	if sql != lockBillSQL {
		return totalsRow{err: fmt.Errorf("unexpected QueryRow %q", sql)}
	}
	return t.lock
}

func (t *totalsTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.record(sql, args)
	if err := t.queryErr[sql]; err != nil {
		return nil, err
	}
	switch sql {
	case clientBillsSQL:
		return &totalsRows{rows: t.bills}, nil
	case approvedWriteOffsSQL:
		return &totalsRows{rows: t.approved}, nil
	case pendingWriteOffsSQL:
		return &totalsRows{rows: t.pending}, nil
	}
	return nil, fmt.Errorf("unexpected Query %q", sql)
}

func (t *totalsTx) record(sql string, args []any) {
	t.calls = append(t.calls, sql)
	if t.args == nil {
		t.args = map[string][]any{}
	}
	t.args[sql] = args
}

// rule: BIL-C-085
func TestBIL_C_085_PGWriteOffTotals(t *testing.T) {
	since := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	inWindow, before := since.Add(24*time.Hour), since.Add(-time.Hour)
	scope := WriteOffScope{BillID: "b1", ClientID: "c1", Since: since, ExcludeID: "w-new"}
	clientBills := [][]any{{"b1"}, {"b2"}}
	boom := errors.New("connection reset")
	tests := []struct {
		name  string
		scope WriteOffScope
		tx    *totalsTx
		want  int64
		err   error
	}{
		{name: "nothing earlier", scope: scope, tx: &totalsTx{lock: totalsRow{vals: []any{"c1"}}, bills: clientBills}},
		{name: "approved write-offs summed", scope: scope, want: 150000,
			tx: &totalsTx{lock: totalsRow{vals: []any{"c1"}}, bills: clientBills,
				approved: [][]any{{"w1", int64(100000)}, {"w2", int64(50000)}}}},
		{name: "the write-off being decided never counts", scope: scope, want: 100000,
			tx: &totalsTx{lock: totalsRow{vals: []any{"c1"}}, bills: clientBills,
				approved: [][]any{{"w1", int64(100000)}},
				pending:  [][]any{{"w-new", true, "b1", int64(59000), inWindow}}}},
		{name: "pending on the bill counts once across both gates", scope: scope, want: 70000,
			tx: &totalsTx{lock: totalsRow{vals: []any{"c1"}}, bills: clientBills,
				pending: [][]any{{"w3", true, "b1", int64(70000), before}, {"w3", true, "b1", int64(70000), inWindow}}}},
		{name: "approved and pending on one write-off count once", scope: scope, want: 40000,
			tx: &totalsTx{lock: totalsRow{vals: []any{"c1"}}, bills: clientBills,
				approved: [][]any{{"w4", int64(40000)}},
				pending:  [][]any{{"w4", true, "b1", int64(40000), inWindow}}}},
		{name: "client's other bill in window counts, outside does not", scope: scope, want: 20000,
			tx: &totalsTx{lock: totalsRow{vals: []any{"c1"}}, bills: clientBills,
				pending: [][]any{{"w5", true, "b2", int64(20000), inWindow}, {"w6", true, "b2", int64(30000), before}}}},
		{name: "another client's bill does not count", scope: scope,
			tx: &totalsTx{lock: totalsRow{vals: []any{"c1"}}, bills: clientBills,
				pending: [][]any{{"w7", true, "b9", int64(20000), inWindow}}}},
		{name: "unknown bill", scope: scope, err: ErrNotFound, tx: &totalsTx{lock: totalsRow{err: pgx.ErrNoRows}}},
		{name: "malformed bill id", scope: scope, err: ErrNotFound, tx: &totalsTx{lock: totalsRow{err: &pgconn.PgError{Code: "22P02"}}}},
		{name: "lock error", scope: scope, err: boom, tx: &totalsTx{lock: totalsRow{err: boom}}},
		{name: "client is not the payer", scope: scope, err: ErrInvalidState, tx: &totalsTx{lock: totalsRow{vals: []any{"c2"}}}},
		{name: "incomplete scope", scope: WriteOffScope{BillID: "b1"}, err: ErrInvalidState, tx: &totalsTx{}},
		{name: "pending gate without stored payload", scope: scope, err: ErrInvalidState,
			tx: &totalsTx{lock: totalsRow{vals: []any{"c1"}}, bills: clientBills,
				pending: [][]any{{"w8", false, "", int64(-1), inWindow}}}},
		{name: "pending payload without amount", scope: scope, err: ErrInvalidState,
			tx: &totalsTx{lock: totalsRow{vals: []any{"c1"}}, bills: clientBills,
				pending: [][]any{{"w8", true, "b1", int64(-1), inWindow}}}},
		{name: "approved query error", scope: scope, err: boom,
			tx: &totalsTx{lock: totalsRow{vals: []any{"c1"}}, bills: clientBills, queryErr: map[string]error{approvedWriteOffsSQL: boom}}},
		{name: "pending query error", scope: scope, err: boom,
			tx: &totalsTx{lock: totalsRow{vals: []any{"c1"}}, bills: clientBills, queryErr: map[string]error{pendingWriteOffsSQL: boom}}},
		{name: "client bills error", scope: scope, err: boom,
			tx: &totalsTx{lock: totalsRow{vals: []any{"c1"}}, queryErr: map[string]error{clientBillsSQL: boom}}},
		{name: "overflow", scope: scope, err: ErrInvalidState,
			tx: &totalsTx{lock: totalsRow{vals: []any{"c1"}}, bills: clientBills,
				approved: [][]any{{"w1", int64(1 << 62)}, {"w2", int64(1 << 62)}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PGWriteOffTotals{}.CumulativeCents(context.Background(), tt.tx, tt.scope)
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("err = %v, want %v", err, tt.err)
				}
				if got != 0 {
					t.Fatalf("total on error = %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("CumulativeCents: %v", err)
			}
			if got != tt.want {
				t.Fatalf("total = %d, want %d", got, tt.want)
			}
			// The bill row is locked before anything is read.
			if len(tt.tx.calls) == 0 || tt.tx.calls[0] != lockBillSQL {
				t.Fatalf("first statement = %v, want the bill lock", tt.tx.calls)
			}
			if a := tt.tx.args[lockBillSQL]; len(a) != 1 || a[0] != "b1" {
				t.Fatalf("lock args = %v", a)
			}
			if a := tt.tx.args[pendingWriteOffsSQL]; len(a) != 1 || a[0] != SubjectWriteOff {
				t.Fatalf("pending args = %v", a)
			}
		})
	}
}

func TestPGWriteOffTotalsLocksBillRow(t *testing.T) {
	if !strings.Contains(lockBillSQL, "FROM billing.bill") || !strings.HasSuffix(strings.TrimSpace(lockBillSQL), "FOR UPDATE") {
		t.Fatalf("lockBillSQL does not lock the bill row: %q", lockBillSQL)
	}
	if !strings.Contains(pendingWriteOffsSQL, "status = 'pending'") {
		t.Fatalf("pending SQL lacks the pending predicate: %q", pendingWriteOffsSQL)
	}
	if _, err := (PGWriteOffTotals{}).CumulativeCents(context.Background(), nil, WriteOffScope{BillID: "b1", ClientID: "c1", Since: time.Now()}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("nil tx err = %v", err)
	}
}
