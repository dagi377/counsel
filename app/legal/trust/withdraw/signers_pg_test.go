package withdraw

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
)

// grantRow is one scripted trust.signatory row as the signatory query returns it.
type grantRow struct {
	licensee   bool
	bonded     bool
	bondExpiry *time.Time
	validFrom  time.Time
	validTo    *time.Time
}

// grantTx answers the signatory query with its rows, or fails with err. It records the
// account and user the query asked for.
type grantTx struct {
	pgx.Tx
	rows []grantRow
	err  error
	args []any
}

func (t *grantTx) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	t.args = args
	if t.err != nil {
		return nil, t.err
	}
	return &grantRows{rows: t.rows, i: -1}, nil
}

type grantRows struct {
	pgx.Rows
	rows []grantRow
	i    int
}

func (r *grantRows) Next() bool { r.i++; return r.i < len(r.rows) }
func (r *grantRows) Close()     {}
func (r *grantRows) Err() error { return nil }
func (r *grantRows) Scan(dest ...any) error {
	x := r.rows[r.i]
	*dest[0].(*string) = "u1"
	*dest[1].(*bool) = x.licensee
	*dest[2].(*bool) = x.bonded
	*dest[3].(**time.Time) = x.bondExpiry
	*dest[4].(*time.Time) = x.validFrom
	*dest[5].(**time.Time) = x.validTo
	return nil
}

func TestPGSignersIsSignatory(t *testing.T) {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	ctx := clock.WithClock(context.Background(), clockfake.New(time.Date(2026, 10, 11, 12, 0, 0, 0, loc), loc))
	today := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	yesterday, lastYear := today.AddDate(0, 0, -1), today.AddDate(-1, 0, 0)
	ptr := func(t time.Time) *time.Time { return &t }
	dbErr := errors.New("connection reset")
	tests := []struct {
		name    string
		rows    []grantRow
		err     error
		want    bool
		wantErr error
	}{
		{name: "valid licensee", rows: []grantRow{{licensee: true, validFrom: lastYear}}, want: true},
		{name: "expired row", rows: []grantRow{{licensee: true, validFrom: lastYear, validTo: ptr(yesterday)}}},
		{name: "frozen today", rows: []grantRow{{licensee: true, validFrom: lastYear, validTo: ptr(today)}}},
		{name: "bond-expired non-licensee", rows: []grantRow{{bonded: true, bondExpiry: ptr(yesterday), validFrom: lastYear}}},
		{name: "bonded non-licensee", rows: []grantRow{{bonded: true, bondExpiry: ptr(today), validFrom: lastYear}}, want: true},
		{name: "no grant"},
		{name: "malformed id", err: &pgconn.PgError{Code: "22P02"}},
		{name: "read error", err: dbErr, wantErr: dbErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &grantTx{rows: tt.rows, err: tt.err}
			got, err := PGSigners{}.IsSignatory(ctx, tx, trust.AccountID("a1"), "u1")
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Fatalf("IsSignatory = %v, %v; want %v, %v", got, err, tt.want, tt.wantErr)
			}
			if len(tx.args) < 3 || tx.args[0] != "a1" || tx.args[2] != "u1" {
				t.Fatalf("query args = %v, want account a1 and user u1", tx.args)
			}
		})
	}
}
