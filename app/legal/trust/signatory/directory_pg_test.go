package signatory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
)

// userRow is the scripted core.app_user row: (id, licensee, active), or err.
type userRow struct {
	id       string
	licensee bool
	active   bool
	err      error
}

func (r userRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*string) = r.id
	*dest[1].(*bool) = r.licensee
	*dest[2].(*bool) = r.active
	return nil
}

// stubTx answers every QueryRow with row and records the SQL and args it saw.
type stubTx struct {
	pgx.Tx
	row  userRow
	sql  []string
	args [][]any
}

func (t *stubTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	t.sql = append(t.sql, sql)
	t.args = append(t.args, args)
	return t.row
}

// stubDB runs fn on tx and returns its error, like a real InTx.
type stubDB struct {
	db.DB
	tx *stubTx
}

func (d stubDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, d.tx)
}

func TestPGDirectoryUser(t *testing.T) {
	boom := errors.New("connection reset")
	tests := []struct {
		name    string
		row     userRow
		want    User
		err     error
		wantErr bool
	}{
		{name: "active licensee", row: userRow{id: "u1", licensee: true, active: true},
			want: User{ID: "u1", Licensee: true, Active: true}},
		{name: "active non-licensee", row: userRow{id: "u1", active: true}, want: User{ID: "u1", Active: true}},
		{name: "deactivated licensee", row: userRow{id: "u1", licensee: true}, want: User{ID: "u1", Licensee: true}},
		{name: "not found", row: userRow{err: pgx.ErrNoRows}, err: ErrNotFound},
		{name: "malformed id", row: userRow{err: &pgconn.PgError{Code: "22P02"}}, err: ErrNotFound},
		{name: "read error wrapped", row: userRow{err: boom}, err: boom},
		{name: "other pg error not masked", row: userRow{err: &pgconn.PgError{Code: "57014"}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &stubTx{row: tt.row}
			got, err := NewPGDirectory(stubDB{tx: tx}).User(context.Background(), "u1")
			switch {
			case tt.err != nil:
				if !errors.Is(err, tt.err) {
					t.Fatalf("err = %v, want %v", err, tt.err)
				}
			case tt.wantErr:
				if err == nil || errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want a non-not-found error", err)
				}
			case err != nil:
				t.Fatalf("err = %v", err)
			}
			if got != tt.want {
				t.Fatalf("user = %+v, want %+v", got, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "u1") {
				t.Fatalf("error leaks the user id: %v", err)
			}
			if len(tx.sql) != 1 || !strings.Contains(tx.sql[0], "FROM core.app_user") {
				t.Fatalf("sql = %q", tx.sql)
			}
			if len(tx.args[0]) != 1 || tx.args[0][0] != "u1" {
				t.Fatalf("args = %v", tx.args[0])
			}
		})
	}
}

// The licensee and active predicates are what keep a suspended or departed user from
// signing; pin them so a loosened query fails here.
func TestPGDirectoryPredicates(t *testing.T) {
	for _, want := range []string{
		"principal_kind = 'human'",
		"licence_type IN ('lawyer', 'paralegal')",
		"licence_status = 'good_standing'",
		"status = 'active'",
		"offboarded_at IS NULL",
		"archived_at IS NULL",
		"expires_on",
		"America/Toronto",
	} {
		if !strings.Contains(directoryUserSQL, want) {
			t.Errorf("directoryUserSQL lacks %q", want)
		}
	}
}
