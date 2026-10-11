package ledger

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// glTx extends fakeTx so Balance can read back what Post wrote: every account code gets
// its own id, and the balance statements sum the recorded line inserts by entry date.
type glTx struct {
	*fakeTx
	entryDate time.Time // date of the entry sqlInsertEntry created
}

func (g *glTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	switch sql {
	case sqlAccountID:
		return fakeRow{vals: []any{"id-" + args[0].(string)}}
	case sqlInsertEntry:
		g.entryDate = args[0].(time.Time)
	case sqlBalanceAccount:
		code := args[0].(string)
		if code == "9999" {
			return fakeRow{err: pgx.ErrNoRows}
		}
		return fakeRow{vals: []any{"id-" + code}}
	case sqlBalanceSum:
		id, asOf := args[0].(string), args[1].(time.Time)
		var sum int64
		for _, e := range g.execs {
			if e.sql != sqlInsertLine || e.args[1] != id || g.entryDate.After(asOf) {
				continue
			}
			sum += e.args[2].(int64) - e.args[3].(int64)
		}
		return fakeRow{vals: []any{sum}}
	}
	return g.fakeTx.QueryRow(ctx, sql, args...)
}

func TestGLPostThenBalance(t *testing.T) {
	d := func(day int) Date { return Date{Year: 2026, Month: time.March, Day: day} }
	e := Entry{Book: "main", Date: d(15), SourceKind: "manual", SourceID: "s1",
		Lines: []Line{line("1000", Debit, 700), line("4000", Credit, 700)}}
	tests := []struct {
		name    string
		account string
		asOf    Date
		want    Cents
		err     error
	}{
		{"debit account after posting", "1000", d(31), 700, nil},
		{"credit account after posting", "4000", d(31), -700, nil},
		{"same day as entry", "1000", d(15), 700, nil},
		{"before entry date", "1000", d(14), 0, nil},
		{"untouched account", "2000", d(31), 0, nil},
		{"unknown account", "9999", d(31), 0, ErrUnknownAccount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tx := &glTx{fakeTx: &fakeTx{}}
			gl, err := NewGL(&PG{PostedBy: poster}, Balances{Q: tx})
			if err != nil {
				t.Fatalf("NewGL: %v", err)
			}
			var l Ledger = gl
			if _, err := l.Post(ctx, tx, e); err != nil {
				t.Fatalf("Post: %v", err)
			}
			got, err := l.Balance(ctx, tt.account, tt.asOf)
			if !errors.Is(err, tt.err) || got != tt.want {
				t.Fatalf("Balance(%s, %v) = %d, %v; want %d, %v", tt.account, tt.asOf, got, err, tt.want, tt.err)
			}
		})
	}
}

func TestNewGLRefusesMissingHalf(t *testing.T) {
	tests := []struct {
		name string
		pg   *PG
		b    Balances
		err  error
	}{
		{"both present", &PG{PostedBy: poster}, Balances{Q: &fakeTx{}}, nil},
		{"nil PG", nil, Balances{Q: &fakeTx{}}, ErrIncompleteGL},
		{"nil Balances querier", &PG{PostedBy: poster}, Balances{}, ErrIncompleteGL},
		{"both missing", nil, Balances{}, ErrIncompleteGL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gl, err := NewGL(tt.pg, tt.b)
			if !errors.Is(err, tt.err) {
				t.Fatalf("NewGL err = %v, want %v", err, tt.err)
			}
			if err != nil && (gl.PG != nil || gl.Q != nil) {
				t.Fatalf("NewGL returned a partial GL on error: %+v", gl)
			}
		})
	}
}
