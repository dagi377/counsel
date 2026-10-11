package timekeeping

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// tkRows yields ids; every other pgx.Rows method is unused.
type tkRows struct {
	pgx.Rows
	ids []string
	i   int
}

func (r *tkRows) Next() bool { r.i++; return r.i <= len(r.ids) }
func (r *tkRows) Scan(dest ...any) error {
	*dest[0].(*string) = r.ids[r.i-1]
	return nil
}
func (r *tkRows) Err() error { return nil }
func (r *tkRows) Close()     {}

// tkTx answers Query with ids (or err) and records the SQL and arguments it was given.
type tkTx struct {
	pgx.Tx
	ids  []string
	err  error
	sql  string
	args []any
}

func (t *tkTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.sql, t.args = sql, args
	if t.err != nil {
		return nil, t.err
	}
	return &tkRows{ids: t.ids}, nil
}

// tkDB runs InTx on its one tkTx.
type tkDB struct {
	db.DB
	tx *tkTx
}

func (d tkDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, d.tx)
}

// TestPGTimekeepers lists the active timekeepers: the query asks for time.record holders
// that are active humans with an active role, and passes the ids through in order.
//
// rule: LSO-3.6
func TestPGTimekeepers(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name    string
		db      db.DB
		tx      *tkTx
		want    []string
		wantErr error
	}{
		{"no database", nil, nil, nil, ErrNoDatabase},
		{"timekeepers", nil, &tkTx{ids: []string{"u-a", "u-b"}}, []string{"u-a", "u-b"}, nil},
		{"nobody", nil, &tkTx{}, nil, nil},
		{"query fails", nil, &tkTx{err: boom}, nil, boom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := PGTimekeepers{DB: tc.db}
			if tc.tx != nil {
				p.DB = tkDB{tx: tc.tx}
			}
			got, err := p.ActiveUsers(context.Background(), "F-TIM-01")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("users = %v, want %v", got, tc.want)
			}
			if tc.tx == nil {
				return
			}
			if len(tc.tx.args) != 1 || tc.tx.args[0] != CapRecord {
				t.Fatalf("args = %v, want [%s]", tc.tx.args, CapRecord)
			}
			for _, clause := range []string{"u.status = 'active'", "u.principal_kind = 'human'", "r.status = 'active'", "g.level <> 'none'", "u.expires_on"} {
				if !strings.Contains(tc.tx.sql, clause) {
					t.Errorf("query lacks %q", clause)
				}
			}
		})
	}
}
