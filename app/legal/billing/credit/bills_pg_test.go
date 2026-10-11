package credit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// billTx answers billSQL with row and records its arguments.
type billTx struct {
	pgx.Tx
	row  valsRow
	args []any
}

func (t *billTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if !strings.Contains(sql, "billing.bill_balance") {
		return valsRow{err: errors.New("unexpected query")}
	}
	t.args = args
	return t.row
}

// billRunner runs fn on tx.
type billRunner struct{ tx pgx.Tx }

func (r billRunner) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, r.tx)
}

type stubLawyers struct {
	lawyer string
	err    error
}

func (s stubLawyers) ResponsibleLawyer(context.Context, pgx.Tx, string) (string, error) {
	return s.lawyer, s.err
}

// rule: BIL-C-082
// PGBills reports the stored open balance unclamped: a negative balance stays negative, so
// the request route refuses any credit against it.
func TestBIL_C_082_PGBillsReadsOpenBalance(t *testing.T) {
	tests := []struct {
		name    string
		row     valsRow
		lawyers ResponsibleLawyers
		want    Bill
		wantErr error
	}{
		{"delivered", valsRow{vals: []any{"b1", "m1", true, int64(11300)}}, nil,
			Bill{ID: "b1", Delivered: true, OpenCents: 11300}, nil},
		{"negative balance is not clamped", valsRow{vals: []any{"b1", "m1", true, int64(-500)}}, nil,
			Bill{ID: "b1", Delivered: true, OpenCents: -500}, nil},
		{"not delivered", valsRow{vals: []any{"b1", "m1", false, int64(100)}}, nil,
			Bill{ID: "b1", OpenCents: 100}, nil},
		{"with responsible lawyer", valsRow{vals: []any{"b1", "m1", true, int64(1)}}, stubLawyers{lawyer: "law"},
			Bill{ID: "b1", Delivered: true, OpenCents: 1, ResponsibleUser: "law"}, nil},
		{"lawyer lookup fails closed", valsRow{vals: []any{"b1", "m1", true, int64(1)}}, stubLawyers{err: ErrNotFound},
			Bill{}, ErrNotFound},
		{"unknown bill", valsRow{err: pgx.ErrNoRows}, nil, Bill{}, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &billTx{row: tt.row}
			got, err := PGBills{DB: billRunner{tx: tx}, Lawyers: tt.lawyers}.Bill(context.Background(), "b1")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Bill err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("Bill = %+v, want %+v", got, tt.want)
			}
			if len(tx.args) != 1 || tx.args[0] != "b1" {
				t.Fatalf("args = %v", tx.args)
			}
		})
	}
}

func TestPGBillsNoDatabase(t *testing.T) {
	if _, err := (PGBills{}).Bill(context.Background(), "b1"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("err = %v, want ErrInvalidState", err)
	}
}
