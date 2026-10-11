package matter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/app/legal/party/partyfake"
	"github.com/dagi377/counsel/kernel/db"
)

// stubRow scans fixed values, or returns err.
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
		case **time.Time:
			*p = nil
		case *time.Time:
			*p = r.vals[i].(time.Time)
		case *int:
			*p = r.vals[i].(int)
		}
	}
	return nil
}

// stubTx answers QueryRow with row and records the SQL arguments. Other pgx.Tx methods panic.
type stubTx struct {
	pgx.Tx
	row  stubRow
	args []any
}

func (t *stubTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	t.args = args
	return t.row
}

// stubDB hands tx to every InTx.
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

func TestCreate(t *testing.T) {
	ctx := context.Background()
	created := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	contacts := partyfake.New(func() time.Time { return created })
	client, err := contacts.Create(ctx, party.NewContact{Kind: party.Person, LegalName: "A", DisplayName: "A", IsClient: true})
	if err != nil {
		t.Fatal(err)
	}
	valid := NewMatter{Number: "2026-0001", ShortName: "x", ClientID: client.ID, PracticeAreaID: "pa", ResponsibleLawyerID: "u1", CreatedBy: "u2"}
	okRow := stubRow{vals: []any{"m1", "pending", nil, created, 1}}
	tests := []struct {
		name    string
		in      func(NewMatter) NewMatter
		row     stubRow
		err     error
		inserts int
		vis     Visibility
	}{
		{name: "creates pending firm_open", in: func(m NewMatter) NewMatter { return m }, row: okRow, inserts: 1, vis: FirmOpen},
		{name: "keeps visibility", in: func(m NewMatter) NewMatter { m.Visibility = Restricted; return m }, row: okRow, inserts: 1, vis: Restricted},
		{name: "missing number", in: func(m NewMatter) NewMatter { m.Number = ""; return m }, err: ErrInvalid},
		{name: "missing lawyer", in: func(m NewMatter) NewMatter { m.ResponsibleLawyerID = ""; return m }, err: ErrInvalid},
		{name: "bad visibility", in: func(m NewMatter) NewMatter { m.Visibility = "public"; return m }, err: ErrInvalid},
		{name: "unknown client", in: func(m NewMatter) NewMatter { m.ClientID = "nope"; return m }, err: ErrUnknownClient},
		{name: "duplicate number", in: func(m NewMatter) NewMatter { return m }, row: stubRow{err: &pgconn.PgError{Code: pgUniqueViolation}}, inserts: 1, err: ErrDuplicate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &stubDB{tx: &stubTx{row: tt.row}}
			got, err := NewStore(d, contacts).Create(ctx, tt.in(valid))
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if d.calls != tt.inserts {
				t.Fatalf("transactions = %d, want %d", d.calls, tt.inserts)
			}
			if tt.err != nil {
				return
			}
			if got.ID != "m1" || got.Status != Pending || got.Visibility != tt.vis || !got.OpenDate.IsZero() ||
				got.RetentionClass != DefaultRetentionClass || got.RowVersion != 1 || !got.CreatedAt.Equal(created) {
				t.Fatalf("matter = %+v", got)
			}
			if a := d.tx.args; a[2] != string(client.ID) || a[5] != string(tt.vis) || a[7] != "u2" {
				t.Fatalf("insert args = %v", a)
			}
		})
	}
}

func TestAddMember(t *testing.T) {
	ctx := context.Background()
	valid := NewMember{MatterID: "m1", UserID: "u1", GrantedBy: "u2"}
	tests := []struct {
		name string
		in   func(NewMember) NewMember
		row  stubRow
		err  error
		role MemberRole
	}{
		{name: "default member role", in: func(m NewMember) NewMember { return m }, row: stubRow{vals: []any{"mm1"}}, role: RoleMember},
		{name: "reviewer", in: func(m NewMember) NewMember { m.Role = RoleReviewer; return m }, row: stubRow{vals: []any{"mm1"}}, role: RoleReviewer},
		{name: "self grant", in: func(m NewMember) NewMember { m.GrantedBy = m.UserID; return m }, err: ErrSelfGrant},
		{name: "missing user", in: func(m NewMember) NewMember { m.UserID = ""; return m }, err: ErrInvalid},
		{name: "bad role", in: func(m NewMember) NewMember { m.Role = "owner"; return m }, err: ErrInvalid},
		{name: "unknown matter", in: func(m NewMember) NewMember { return m }, row: stubRow{err: pgx.ErrNoRows}, err: ErrNotFound},
		{name: "already active", in: func(m NewMember) NewMember { return m }, row: stubRow{err: &pgconn.PgError{Code: pgUniqueViolation}}, err: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &stubDB{tx: &stubTx{row: tt.row}}
			got, err := NewStore(d, nil).AddMember(ctx, tt.in(valid))
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if tt.err != nil {
				return
			}
			if got.ID != "mm1" || got.Role != tt.role || got.MatterID != "m1" || got.GrantedBy != "u2" {
				t.Fatalf("member = %+v", got)
			}
		})
	}
}
