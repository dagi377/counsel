package party

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
)

// stubRow scans fixed values in order, or returns err.
type stubRow struct {
	vals []any
	err  error
}

func (r stubRow) Scan(dest ...any) error {
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
		case *int:
			*p = r.vals[i].(int)
		}
	}
	return nil
}

// stubTx answers QueryRow with row and records the arguments. Other pgx.Tx methods panic.
type stubTx struct {
	pgx.Tx
	row  stubRow
	args []any
}

func (t *stubTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	t.args = args
	return t.row
}

type stubDB struct {
	tx    *stubTx
	calls int
}

func (d *stubDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	d.calls++
	return fn(ctx, d.tx)
}
func (d *stubDB) Bind(context.Context, pgx.Tx, db.Session) error { return nil }
func (d *stubDB) Ping(context.Context) error                     { return nil }
func (d *stubDB) Close()                                         {}

var created = time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)

func TestCreateContact(t *testing.T) {
	valid := NewContact{Kind: Person, LegalName: "L", DisplayName: "D", IsClient: true, CreatedBy: "u1"}
	okRow := stubRow{vals: []any{"c1", "proposed", created, 1}}
	tests := []struct {
		name  string
		in    func(NewContact) NewContact
		err   error
		calls int
		lang  string
	}{
		{name: "person defaults to en", in: func(c NewContact) NewContact { return c }, calls: 1, lang: "en"},
		{name: "organization in fr", in: func(c NewContact) NewContact { c.Kind = Organization; c.PreferredLanguage = "fr"; return c }, calls: 1, lang: "fr"},
		{name: "bad kind", in: func(c NewContact) NewContact { c.Kind = "robot"; return c }, err: ErrInvalid},
		{name: "no legal name", in: func(c NewContact) NewContact { c.LegalName = ""; return c }, err: ErrInvalid},
		{name: "no display name", in: func(c NewContact) NewContact { c.DisplayName = ""; return c }, err: ErrInvalid},
		{name: "bad language", in: func(c NewContact) NewContact { c.PreferredLanguage = "de"; return c }, err: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &stubDB{tx: &stubTx{row: okRow}}
			in := tt.in(valid)
			got, err := NewStore(d).Create(context.Background(), in)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if d.calls != tt.calls {
				t.Fatalf("transactions = %d, want %d", d.calls, tt.calls)
			}
			if tt.err != nil {
				return
			}
			want := Contact{ID: "c1", Kind: in.Kind, LegalName: "L", DisplayName: "D", PreferredLanguage: tt.lang, IsClient: true, State: Proposed, CreatedAt: created, RowVersion: 1}
			if got != want {
				t.Fatalf("contact = %+v, want %+v", got, want)
			}
			if a := d.tx.args; a[3] != tt.lang || a[5] != "u1" {
				t.Fatalf("insert args = %v", a)
			}
		})
	}
}

func TestGetContact(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		id   ContactID
		row  stubRow
		err  error
	}{
		{name: "found", id: "c1", row: stubRow{vals: []any{"c1", "organization", "L", "D", "fr", false, "active", created, 3}}},
		{name: "no rows", id: "c2", row: stubRow{err: pgx.ErrNoRows}, err: ErrNotFound},
		{name: "not a uuid", id: "x", row: stubRow{err: &pgconn.PgError{Code: pgInvalidText}}, err: ErrNotFound},
		{name: "empty id", id: "", err: ErrNotFound},
		{name: "db error", id: "c1", row: stubRow{err: boom}, err: boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &stubDB{tx: &stubTx{row: tt.row}}
			got, err := NewStore(d).Get(context.Background(), tt.id)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if tt.err != nil {
				return
			}
			want := Contact{ID: "c1", Kind: Organization, LegalName: "L", DisplayName: "D", PreferredLanguage: "fr", State: Active, CreatedAt: created, RowVersion: 3}
			if got != want {
				t.Fatalf("contact = %+v, want %+v", got, want)
			}
		})
	}
}
