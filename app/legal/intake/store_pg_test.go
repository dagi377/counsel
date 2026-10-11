package intake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// pgRow scans fixed values in order, or returns err.
type pgRow struct {
	vals []any
	err  error
}

func (r pgRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *string:
			*p = r.vals[i].(string)
		case *bool:
			*p = r.vals[i].(bool)
		case *time.Time:
			*p = r.vals[i].(time.Time)
		}
	}
	return nil
}

// pgTx answers each QueryRow with the next row.
type pgTx struct {
	pgx.Tx
	rows []pgRow
}

func (t *pgTx) QueryRow(context.Context, string, ...any) pgx.Row {
	r := t.rows[0]
	t.rows = t.rows[1:]
	return r
}

type pgDB struct{ tx *pgTx }

func (d pgDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, d.tx)
}
func (d pgDB) Bind(context.Context, pgx.Tx, db.Session) error { return nil }
func (d pgDB) Ping(context.Context) error                     { return nil }
func (d pgDB) Close()                                         {}

func TestPGLeadsInsert(t *testing.T) {
	at := time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC)
	stored := pgRow{vals: []any{"l1", "f1", "c1", "open", at, ""}}
	first := pgRow{vals: []any{"l0", "f1", "c0", "open", at, ""}}
	tests := []struct {
		name    string
		rows    []pgRow
		key     string
		wantID  string
		wantErr bool
	}{
		{"new lead", []pgRow{stored}, "k1", "l1", false},
		{"key already used returns the first lead", []pgRow{{err: pgx.ErrNoRows}, first}, "k1", "l0", false},
		{"no key and no row fails", []pgRow{{err: pgx.ErrNoRows}}, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := PGLeads{DB: pgDB{tx: &pgTx{rows: tt.rows}}}
			l, err := s.Insert(context.Background(), Lead{FormID: "f1", ContactID: "c1", CreatedAt: at}, tt.key)
			if (err != nil) != tt.wantErr || l.ID != tt.wantID {
				t.Fatalf("Insert = %+v, %v", l, err)
			}
		})
	}
}

func TestPGFormsForm(t *testing.T) {
	tests := []struct {
		name    string
		row     pgRow
		wantPub bool
		wantErr error
	}{
		{"published form", pgRow{vals: []any{"f1", "Intake", true}}, true, nil},
		{"missing form", pgRow{err: pgx.ErrNoRows}, false, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := PGForms{DB: pgDB{tx: &pgTx{rows: []pgRow{tt.row}}}}.Form(context.Background(), "f1")
			if !errors.Is(err, tt.wantErr) || f.Published != tt.wantPub {
				t.Fatalf("Form = %+v, %v", f, err)
			}
		})
	}
}
